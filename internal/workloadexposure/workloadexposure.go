// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

// Package workloadexposure turns the exposures a workload descriptor asks for into the
// Endpoints of a component's line. It resolves each exposure's alias against the
// ComponentType's allowedEndpoints and validates its parameters against the EndpointType, so
// every client that applies a descriptor creates the same Endpoints the portal would.
package workloadexposure

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"unicode/utf16"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/openchoreo/openchoreo/api/v1alpha1"
	"github.com/openchoreo/openchoreo/internal/labels"
	endpointvalidation "github.com/openchoreo/openchoreo/internal/validation/endpoint"
)

// ManagedByWorkloadDescriptor is the openchoreo.dev/managed-by value of an Endpoint created
// from a workload descriptor. Applying the descriptor again updates or deletes only
// Endpoints carrying it, and the portal shows them read-only.
const ManagedByWorkloadDescriptor = "workload-descriptor"

// Exposure is one Endpoint a workload descriptor asks for, unresolved.
type Exposure struct {
	// Endpoint is the workload endpoint to expose.
	Endpoint string
	// Name tells apart the Endpoints of one workload endpoint; empty for at most one of them.
	Name       string
	Visibility []v1alpha1.EndpointVisibility
	// Via names an allowedEndpoints entry; empty selects the protocol's default entry.
	Via        string
	Parameters *runtime.RawExtension
}

// Target is the component line whose Endpoints are generated.
type Target struct {
	Namespace     string
	Project       string
	Component     string
	ReleaseLine   string // empty for the main line
	ComponentType v1alpha1.ComponentTypeRef
	// AutoDeploy is the line's effective autoDeploy, which its Endpoints follow.
	AutoDeploy bool
	// Endpoints are the workload endpoints being applied with the exposures.
	Endpoints map[string]v1alpha1.WorkloadEndpoint
}

// Lookup reads the platform engineer's types an exposure resolves against.
type Lookup interface {
	// AllowedEndpoints returns the allowedEndpoints of a (Cluster)ComponentType.
	AllowedEndpoints(ctx context.Context, namespace string, ref v1alpha1.ComponentTypeRef) ([]v1alpha1.AllowedEndpointType, error)
	// EndpointTypeParameters returns the parameters schema of a (Cluster)EndpointType.
	EndpointTypeParameters(ctx context.Context, namespace string, ref v1alpha1.EndpointTypeRef) (*v1alpha1.SchemaSection, error)
}

// InvalidExposureError reports an exposure that cannot be resolved or whose parameters do not
// fit its EndpointType. It is the developer's to fix, unlike a lookup failure.
type InvalidExposureError struct {
	Msg string
}

func (e *InvalidExposureError) Error() string { return e.Msg }

func invalid(format string, args ...any) error {
	return &InvalidExposureError{Msg: fmt.Sprintf(format, args...)}
}

// Generate resolves exposures into the target's Endpoints, in the order given. It returns an
// *InvalidExposureError for anything the developer has to change in the descriptor.
func Generate(ctx context.Context, target Target, exposures []Exposure, lookup Lookup) ([]v1alpha1.Endpoint, error) {
	if len(exposures) == 0 {
		return nil, nil
	}
	allowed, err := lookup.AllowedEndpoints(ctx, target.Namespace, target.ComponentType)
	if err != nil {
		return nil, err
	}
	if len(allowed) == 0 {
		return nil, invalid("component type %s allows no Endpoint types, so its endpoints cannot be exposed",
			target.ComponentType.Name)
	}

	endpoints := make([]v1alpha1.Endpoint, 0, len(exposures))
	names := make(map[string]string, len(exposures))
	for _, exposure := range exposures {
		subject := exposureSubject(exposure)
		workloadEndpoint, ok := target.Endpoints[exposure.Endpoint]
		if !ok {
			return nil, invalid("%s: the workload has no endpoint %q", subject, exposure.Endpoint)
		}
		if len(exposure.Visibility) == 0 {
			return nil, invalid("%s: at least one visibility is required", subject)
		}
		for _, v := range exposure.Visibility {
			if v == v1alpha1.EndpointVisibilityProject {
				return nil, invalid("%s: project visibility is implicit and cannot be exposed", subject)
			}
		}

		entry, err := resolveEntry(allowed, workloadEndpoint.EffectiveProtocol(), exposure.Via)
		if err != nil {
			return nil, invalid("%s: %v", subject, err)
		}
		section, err := lookup.EndpointTypeParameters(ctx, target.Namespace, entry.Ref)
		if err != nil {
			return nil, err
		}
		if err := endpointvalidation.ValidateValues(section, exposure.Parameters); err != nil {
			return nil, invalid("%s: parameters do not fit %s %s: %v", subject, entry.Ref.Kind, entry.Ref.Name, err)
		}

		name := EndpointName(target.Component, target.ReleaseLine, exposure.Endpoint, exposure.Name)
		if other, taken := names[name]; taken {
			return nil, invalid("%s and %s would both be named %s", other, subject, name)
		}
		names[name] = subject
		endpoints = append(endpoints, build(target, exposure, entry.Ref, name))
	}
	return endpoints, nil
}

// resolveEntry picks the allowedEndpoints entry for a protocol: the one named via, or the
// protocol's default entry when via is empty. Never falls back from a named alias to the
// default, which could expose an endpoint through a weaker type than the one asked for.
func resolveEntry(allowed []v1alpha1.AllowedEndpointType, protocol v1alpha1.EndpointProtocol, via string) (*v1alpha1.AllowedEndpointType, error) {
	var aliases []string
	for i := range allowed {
		entry := &allowed[i]
		if !slices.Contains(entry.Protocols, protocol) {
			continue
		}
		if entry.Name != "" {
			aliases = append(aliases, entry.Name)
		}
		if via == "" && entry.Default {
			return entry, nil
		}
		if via != "" && entry.Name == via {
			return entry, nil
		}
	}
	available := "none"
	if len(aliases) > 0 {
		available = strings.Join(aliases, ", ")
	}
	if via == "" {
		return nil, fmt.Errorf("no default Endpoint type for protocol %s; name one with via (available: %s)", protocol, available)
	}
	return nil, fmt.Errorf("via %q is not an allowed Endpoint type for protocol %s (available: %s)", via, protocol, available)
}

// build makes the Endpoint for one resolved exposure, named and labeled as the portal names and
// labels a component's own Endpoints, plus the managed-by label.
func build(target Target, exposure Exposure, ref v1alpha1.EndpointTypeRef, name string) v1alpha1.Endpoint {
	endpointLabels := map[string]string{
		labels.LabelKeyProjectName:     target.Project,
		labels.LabelKeySourceComponent: target.Component,
		labels.LabelKeySourceEndpoint:  ToDNSLabel(exposure.Endpoint),
		labels.LabelKeyManagedBy:       ManagedByWorkloadDescriptor,
	}
	backendName := target.Component
	if target.ReleaseLine != "" {
		endpointLabels[labels.LabelKeySourceReleaseLine] = target.ReleaseLine
		backendName = target.ReleaseLine
	}

	endpoint := v1alpha1.Endpoint{
		TypeMeta: metav1.TypeMeta{
			APIVersion: v1alpha1.GroupVersion.String(),
			Kind:       "Endpoint",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: target.Namespace,
			Labels:    endpointLabels,
		},
		Spec: v1alpha1.EndpointSpec{
			Owner:      v1alpha1.EndpointOwner{ProjectName: target.Project},
			Type:       ref,
			Visibility: slices.Clone(exposure.Visibility),
			BackendRefs: []v1alpha1.EndpointBackendRef{{
				Name: ToDNSLabel(backendName),
				Kind: v1alpha1.EndpointBackendKindComponent,
				Component: &v1alpha1.EndpointComponentBackend{
					Name:        target.Component,
					Endpoint:    exposure.Endpoint,
					ReleaseLine: target.ReleaseLine,
				},
			}},
			AutoDeploy: target.AutoDeploy,
		},
	}
	if exposure.Parameters != nil && len(exposure.Parameters.Raw) > 0 && string(exposure.Parameters.Raw) != "{}" {
		endpoint.Spec.Parameters = exposure.Parameters.DeepCopy()
	}
	if endpoint.Spec.Type.Kind == "" {
		endpoint.Spec.Type.Kind = v1alpha1.EndpointTypeRefKindEndpointType
	}
	return endpoint
}

func exposureSubject(exposure Exposure) string {
	if exposure.Name == "" {
		return fmt.Sprintf("endpoint %q", exposure.Endpoint)
	}
	return fmt.Sprintf("endpoint %q expose %q", exposure.Endpoint, exposure.Name)
}

// EndpointName names the Endpoint of a component's workload endpoint: `<component>-<endpoint>`
// on the main line and `<component>-<line>-<endpoint>` on a release line, suffixed with the
// exposure's name when it has one. Mirrors the portal's componentEndpointName, so the portal's
// release-line drafts skip an Endpoint a line's descriptor already made.
func EndpointName(component, releaseLine, endpoint, suffix string) string {
	base := component + "-" + endpoint
	if releaseLine != "" {
		base = component + "-" + releaseLine + "-" + endpoint
	}
	if suffix != "" {
		base += "-" + suffix
	}
	return ToDNSLabel(base)
}

const maxNameLength = 63

// ToDNSLabel turns a string into a DNS-1123 label, replacing the tail with a hash of the whole
// value when it has to be shortened. Mirrors the portal's toDnsLabel, hash included: FNV-1a
// over the UTF-16 code units, as 8 hex digits.
func ToDNSLabel(value string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(value) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			if r == '-' {
				if dash {
					continue
				}
				dash = true
			} else {
				dash = false
			}
			b.WriteRune(r)
			continue
		}
		if !dash {
			b.WriteByte('-')
			dash = true
		}
	}
	cleaned := strings.Trim(b.String(), "-")
	if len(cleaned) <= maxNameLength {
		return cleaned
	}
	hash := shortHash(value)
	head := strings.TrimSuffix(cleaned[:maxNameLength-len(hash)-1], "-")
	return head + "-" + hash
}

func shortHash(value string) string {
	hash := uint32(0x811c9dc5)
	for _, unit := range utf16.Encode([]rune(value)) {
		hash ^= uint32(unit)
		hash *= 0x01000193
	}
	return fmt.Sprintf("%08x", hash)
}
