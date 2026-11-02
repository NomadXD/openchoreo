// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// EndpointSpec defines the desired state of Endpoint.
type EndpointSpec struct {
	// Owner identifies the Project this Endpoint belongs to.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="spec.owner is immutable"
	Owner EndpointOwner `json:"owner"`

	// Type references the EndpointType or ClusterEndpointType template for this Endpoint.
	// Kind defaults to EndpointType (namespaced); ClusterEndpointType is also allowed.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="spec.type cannot be changed after creation"
	Type EndpointTypeRef `json:"type"`

	// Visibility lists the scopes this Endpoint exposes its backends to. It is
	// declared rather than inferred from rendered routes, so an EndpointType can
	// render one route per requested visibility and the set of route slots this
	// Endpoint occupies is known before anything is rendered. The project scope
	// is implicit for every workload endpoint and is not a route an Endpoint
	// renders, so it is not accepted here.
	// +kubebuilder:validation:MinItems=1
	// +listType=set
	// +kubebuilder:validation:XValidation:rule="self.all(v, v != 'project')",message="project visibility is implicit for every workload endpoint and cannot be requested on an Endpoint"
	Visibility []EndpointVisibility `json:"visibility"`

	// BackendRefs lists the backends this Endpoint routes traffic to, with
	// relative weights. Like a Service selecting Pods, the Endpoint does not own
	// the referenced components.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=16
	// +listType=map
	// +listMapKey=name
	BackendRefs []EndpointBackendRef `json:"backendRefs"`

	// Schema is the API definition this Endpoint exposes. When unset it resolves
	// from the backend's Workload endpoint schema at render time rather than
	// being copied here, so a new build cannot leave the Endpoint holding a stale
	// contract. Set it explicitly when backends disagree or the exposed surface
	// differs from the backend's.
	// +optional
	Schema *Schema `json:"schema,omitempty"`

	// AutoDeploy deploys the Endpoint to the first environment of its project's
	// deployment pipeline: the Endpoint controller creates the EndpointReleaseBinding
	// for that environment and advances it to each new EndpointRelease. Later
	// environments are promoted explicitly, as with Component.spec.autoDeploy.
	// When not specified, defaults to false (zero value).
	// +optional
	AutoDeploy bool `json:"autoDeploy,omitempty"`

	// Parameters contains values for the parameter schema declared on the referenced
	// EndpointType (or ClusterEndpointType). Validated against the schema by the
	// Endpoint controller; failures surface via status.conditions.
	// +optional
	// +kubebuilder:pruning:PreserveUnknownFields
	// +kubebuilder:validation:Schemaless
	Parameters *runtime.RawExtension `json:"parameters,omitempty"`
}

// EndpointOwner identifies the project that owns an Endpoint.
type EndpointOwner struct {
	// ProjectName is the name of the Project this Endpoint belongs to.
	// +kubebuilder:validation:MinLength=1
	ProjectName string `json:"projectName"`
}

// EndpointBackendKind identifies what an EndpointBackendRef points at.
// +kubebuilder:validation:Enum=Component
type EndpointBackendKind string

const (
	// EndpointBackendKindComponent references a workload endpoint of a Component.
	EndpointBackendKindComponent EndpointBackendKind = "Component"
)

// EndpointBackendRef is one traffic backend of an Endpoint. It is a
// discriminated union on kind; exactly the payload matching kind must be set.
// +kubebuilder:validation:XValidation:rule="self.kind != 'Component' || has(self.component)",message="component must be set when kind is Component"
// +kubebuilder:validation:XValidation:rule="!has(self.match) || self.name != 'default'",message="default names the rule that splits traffic by weight; give a backend with a match another name"
type EndpointBackendRef struct {
	// Name identifies this backend within the Endpoint. It is the list's map key,
	// the handle status reports resolution against, and what a template uses to
	// single out one backend.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	Name string `json:"name"`

	// Kind is the kind of backend this entry references.
	// +optional
	// +kubebuilder:default=Component
	Kind EndpointBackendKind `json:"kind,omitempty"`

	// Component references a workload endpoint of a Component in the Endpoint's
	// project. Required when kind is Component.
	// +optional
	Component *EndpointComponentBackend `json:"component,omitempty"`

	// Weight is the proportion of traffic sent to this backend relative to the
	// sum of all backend weights. Follows Gateway API backendRef weight semantics:
	// 0 sends no traffic to this backend.
	// +optional
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=1000000
	Weight *int32 `json:"weight,omitempty"`

	// Match sends every request that meets all of its conditions to this
	// backend, whatever its weight. Other requests are split by weight, so a
	// backend at weight 0 with a match receives only the requests it matches:
	// testers, a preview path, or an A/B cookie group. When several backends
	// match a request, the first one in the list gets it.
	// +optional
	Match *EndpointMatch `json:"match,omitempty"`
}

// EndpointMatch is the set of conditions a request must all meet.
// +kubebuilder:validation:XValidation:rule="has(self.path) || has(self.headers) || has(self.cookies)",message="set a path, headers or cookies, or remove match"
type EndpointMatch struct {
	// Path the request must be for, relative to where the Endpoint is served:
	// a path of /next on an Endpoint served at /checkout matches /checkout/next.
	// +optional
	Path *EndpointPathMatch `json:"path,omitempty"`

	// Headers the request must carry.
	// +optional
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=8
	Headers []EndpointHeaderMatch `json:"headers,omitempty"`

	// Cookies the request must carry, by exact value. EndpointTypes render a
	// cookie as a match on the Cookie header.
	// +optional
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=1
	Cookies []EndpointCookieMatch `json:"cookies,omitempty"`
}

// EndpointPathMatchType is how a request path is compared.
// +kubebuilder:validation:Enum=PathPrefix;Exact
type EndpointPathMatchType string

const (
	// EndpointPathMatchPathPrefix matches the path and every path below it.
	EndpointPathMatchPathPrefix EndpointPathMatchType = "PathPrefix"
	// EndpointPathMatchExact matches the path only.
	EndpointPathMatchExact EndpointPathMatchType = "Exact"
)

// EndpointPathMatch is a path condition.
type EndpointPathMatch struct {
	// Type is how the path is compared.
	// +optional
	// +kubebuilder:default=PathPrefix
	Type EndpointPathMatchType `json:"type,omitempty"`

	// Value is the path, relative to where the Endpoint is served.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MaxLength=1024
	// +kubebuilder:validation:Pattern=`^/[^?#]*$`
	Value string `json:"value"`
}

// EndpointHeaderMatchType is how a header value is compared.
// +kubebuilder:validation:Enum=Exact;RegularExpression
type EndpointHeaderMatchType string

const (
	// EndpointHeaderMatchExact compares the header value exactly.
	EndpointHeaderMatchExact EndpointHeaderMatchType = "Exact"
	// EndpointHeaderMatchRegularExpression matches the value against an RE2 regular expression.
	EndpointHeaderMatchRegularExpression EndpointHeaderMatchType = "RegularExpression"
)

// EndpointHeaderMatch is a header condition.
type EndpointHeaderMatch struct {
	// Name is the header name, case-insensitive.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	// +kubebuilder:validation:Pattern=`^[A-Za-z0-9!#$%&'*+\-.^_\x60|~]+$`
	Name string `json:"name"`

	// Type is how the value is compared.
	// +optional
	// +kubebuilder:default=Exact
	Type EndpointHeaderMatchType `json:"type,omitempty"`

	// Value is the expected value, or the regular expression.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MaxLength=4096
	Value string `json:"value"`
}

// EndpointCookieMatch is a cookie condition.
type EndpointCookieMatch struct {
	// Name is the cookie name.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`^[A-Za-z0-9_-]{1,64}$`
	Name string `json:"name"`

	// Value is the exact cookie value. Kept to safe characters, because types
	// embed it in a regular expression on the Cookie header.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`^[A-Za-z0-9_.-]{1,128}$`
	Value string `json:"value"`
}

// EndpointComponentBackend references a named workload endpoint of a Component.
type EndpointComponentBackend struct {
	// Name is the name of the Component that serves this backend, in the
	// Endpoint's project.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	Name string `json:"name"`

	// Endpoint is the name of the endpoint in the component's
	// Workload.spec.endpoints to route to. Determines the backend port and protocol.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Endpoint string `json:"endpoint"`

	// ReleaseLine is the component's release line to route to. Empty routes to
	// its main line. Backends of one Endpoint may name different lines of the
	// same component, to split traffic between them.
	// +optional
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	// +kubebuilder:validation:XValidation:rule="self != 'main'",message="main is the implicit line; omit releaseLine for it"
	// +kubebuilder:validation:XValidation:rule="self != 'default'",message="default is a reserved name; omit releaseLine for the main line"
	ReleaseLine string `json:"releaseLine,omitempty"`
}

// EndpointStatus defines the observed state of Endpoint.
type EndpointStatus struct {
	// ObservedGeneration is the most recent generation observed by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions represent the latest available observations of the Endpoint's state.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// LatestRelease is the most recent EndpointRelease for this Endpoint.
	// +optional
	LatestRelease *LatestEndpointRelease `json:"latestRelease,omitempty"`
}

// LatestEndpointRelease identifies the most recent EndpointRelease for an Endpoint.
type LatestEndpointRelease struct {
	// Name of the EndpointRelease resource.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Hash is the content hash of Endpoint.spec + EndpointType/ClusterEndpointType.spec
	// captured at release time.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Hash string `json:"hash"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:printcolumn:name="Project",type=string,JSONPath=`.spec.owner.projectName`
// +kubebuilder:printcolumn:name="Type",type=string,JSONPath=`.spec.type.name`
// +kubebuilder:printcolumn:name="Release",type=string,JSONPath=`.status.latestRelease.name`
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// Endpoint is the Schema for the endpoints API.
// An Endpoint routes traffic across one or more backends using weights,
// rendered through an EndpointType or ClusterEndpointType. The compute side
// (ports, protocols) stays on Workload.spec.endpoints; the network policy that
// admits the Endpoint's visibility to each backend ships with its routes.
// The plural "endpoints" and the short name "ep" are taken by core v1 Endpoints
// in kubectl, so this kind has no short name; use "endpoints.openchoreo.dev".
type Endpoint struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   EndpointSpec   `json:"spec,omitempty"`
	Status EndpointStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// EndpointList contains a list of Endpoint.
type EndpointList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Endpoint `json:"items"`
}

func (e *Endpoint) GetConditions() []metav1.Condition {
	return e.Status.Conditions
}

func (e *Endpoint) SetConditions(conditions []metav1.Condition) {
	e.Status.Conditions = conditions
}

func init() {
	SchemeBuilder.Register(&Endpoint{}, &EndpointList{})
}
