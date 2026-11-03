// Copyright 2025 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package synth

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	openchoreov1alpha1 "github.com/openchoreo/openchoreo/api/v1alpha1"
	"github.com/openchoreo/openchoreo/internal/releaseline"
)

// WorkloadDescriptor represents the structure of a workload.yaml file
// This is the developer-maintained descriptor alongside source code
type WorkloadDescriptor struct {
	APIVersion     string                          `yaml:"apiVersion"`
	Metadata       WorkloadDescriptorMetadata      `yaml:"metadata"`
	Endpoints      []WorkloadDescriptorEndpoint    `yaml:"endpoints,omitempty"`
	Dependencies   *WorkloadDescriptorDependencies `yaml:"dependencies,omitempty"`
	Configurations WorkloadDescriptorConfiguration `yaml:"configurations,omitempty"`
}

type WorkloadDescriptorMetadata struct {
	Name string `yaml:"name"`
}

// Workload descriptor versions. The descriptor is versioned on its own: these name the
// descriptor format, and the Workload CR it converts to stays openchoreo.dev/v1alpha1.
const (
	// DescriptorAPIVersionV1alpha1 is the original format. A descriptor with no apiVersion is
	// read as this version.
	DescriptorAPIVersionV1alpha1 = "openchoreo.dev/v1alpha1"
	// DescriptorAPIVersionV1alpha2 is decoded strictly, requires protocol, and exposes
	// endpoints through expose rather than visibility.
	DescriptorAPIVersionV1alpha2 = "openchoreo.dev/v1alpha2"
)

type WorkloadDescriptorEndpoint struct {
	Name        string `yaml:"name"`
	DisplayName string `yaml:"displayName,omitempty"`
	Port        int32  `yaml:"port"`
	TargetPort  int32  `yaml:"targetPort,omitempty"`
	// Protocol is the protocol the endpoint serves. Required in v1alpha2.
	Protocol string `yaml:"protocol,omitempty"`
	// Type is the v1alpha1 name of Protocol. Deprecated in v1alpha1, rejected in v1alpha2.
	Type       string `yaml:"type,omitempty"`
	BasePath   string `yaml:"basePath,omitempty"`
	SchemaFile string `yaml:"schemaFile,omitempty"`
	Context    string `yaml:"context,omitempty"`
	// Visibility writes the Workload's deprecated endpoint visibility. v1alpha1 only.
	Visibility []string `yaml:"visibility,omitempty"`
	// Expose requests one Endpoint per entry. v1alpha2 only.
	Expose []WorkloadDescriptorExposure `yaml:"expose,omitempty"`
}

// WorkloadDescriptorExposure requests an Endpoint for a workload endpoint.
type WorkloadDescriptorExposure struct {
	// Name tells apart the Endpoints of one workload endpoint. At most one entry may omit it.
	Name string `yaml:"name,omitempty"`
	// Visibility is the Endpoint's visibility: namespace, internal and/or external.
	Visibility []string `yaml:"visibility"`
	// Via names an allowedEndpoints entry of the component's ComponentType. Empty selects the
	// default entry for the endpoint's protocol.
	Via string `yaml:"via,omitempty"`
	// Parameters are the Endpoint's parameters, validated against the resolved EndpointType.
	Parameters map[string]any `yaml:"parameters,omitempty"`
}

// WorkloadExposure is an exposure request read from a v1alpha2 descriptor, as sent to
// openchoreo-api alongside the Workload. It is unresolved: the server maps Via to an
// EndpointType through the component's ComponentType.
type WorkloadExposure struct {
	Endpoint   string         `json:"endpoint"`
	Name       string         `json:"name,omitempty"`
	Visibility []string       `json:"visibility"`
	Via        string         `json:"via,omitempty"`
	Parameters map[string]any `json:"parameters,omitempty"`
}

// DescriptorConversion is the result of converting a workload descriptor.
type DescriptorConversion struct {
	Workload *openchoreov1alpha1.Workload
	// Exposures is nil for a v1alpha1 descriptor and non-nil, possibly empty, for v1alpha2:
	// an empty list asks for every descriptor-managed Endpoint to be removed.
	Exposures []WorkloadExposure
	// Warnings report deprecated descriptor fields.
	Warnings []string
}

// WorkloadDescriptorDependencies represents the dependencies section in workload.yaml
type WorkloadDescriptorDependencies struct {
	// Endpoints define how this workload consumes endpoints from other components.
	Endpoints []WorkloadDescriptorConnection `yaml:"endpoints,omitempty"`
	// Resources define how this workload consumes outputs from project-bound Resources.
	Resources []WorkloadDescriptorResourceDependency `yaml:"resources,omitempty"`
}

// WorkloadDescriptorResourceDependency represents a dependency on a project-bound Resource.
// Output names declared on the referenced ResourceType are wired into the consuming container
// as env vars (envBindings) and file mounts (fileBindings).
type WorkloadDescriptorResourceDependency struct {
	// Ref is the name of the Resource to consume.
	Ref string `yaml:"ref"`
	// EnvBindings maps a ResourceType output name to a container environment variable name.
	EnvBindings map[string]string `yaml:"envBindings,omitempty"`
	// FileBindings maps a ResourceType output name to a container mount path. The referenced
	// output's source kind must be secretKeyRef or configMapKeyRef.
	FileBindings map[string]string `yaml:"fileBindings,omitempty"`
}

type WorkloadDescriptorConnection struct {
	// Project is the target component's project name (optional, defaults to same project).
	Project string `yaml:"project,omitempty"`
	// Component is the target component name.
	Component string `yaml:"component"`
	// Name is the target endpoint name.
	Name string `yaml:"name"`
	// Visibility is the visibility level for the connection.
	Visibility string `yaml:"visibility"`
	// EnvBindings maps connection address components to env var names.
	EnvBindings WorkloadDescriptorConnectionEnvBindings `yaml:"envBindings"`
}

type WorkloadDescriptorConnectionEnvBindings struct {
	Address  string `yaml:"address,omitempty"`
	Host     string `yaml:"host,omitempty"`
	Port     string `yaml:"port,omitempty"`
	BasePath string `yaml:"basePath,omitempty"`
}

// WorkloadDescriptorConfiguration represents the configurations section in workload.yaml
type WorkloadDescriptorConfiguration struct {
	Env   []WorkloadDescriptorEnvVar  `yaml:"env,omitempty"`
	Files []WorkloadDescriptorFileVar `yaml:"files,omitempty"`
}

// WorkloadDescriptorEnvVar represents an environment variable in the descriptor
type WorkloadDescriptorEnvVar struct {
	Name      string                          `yaml:"name"`
	Value     string                          `yaml:"value,omitempty"`
	ValueFrom *WorkloadDescriptorEnvVarSource `yaml:"valueFrom,omitempty"`
}

// WorkloadDescriptorEnvVarSource represents the source for an environment variable value
type WorkloadDescriptorEnvVarSource struct {
	SecretKeyRef *WorkloadDescriptorSecretKeyRef `yaml:"secretKeyRef,omitempty"`
	Path         string                          `yaml:"path,omitempty"`
}

// WorkloadDescriptorSecretKeyRef represents a reference to a secret key
type WorkloadDescriptorSecretKeyRef struct {
	Name string `yaml:"name"`
	Key  string `yaml:"key"`
}

// WorkloadDescriptorFileVar represents a file configuration in the descriptor
type WorkloadDescriptorFileVar struct {
	Name      string                          `yaml:"name"`
	MountPath string                          `yaml:"mountPath"`
	Value     string                          `yaml:"value,omitempty"`
	ValueFrom *WorkloadDescriptorEnvVarSource `yaml:"valueFrom,omitempty"`
}

// ConversionParams holds the parameters needed for workload conversion
type ConversionParams struct {
	NamespaceName string
	ProjectName   string
	ComponentName string
	ImageURL      string
}

// ConvertWorkloadDescriptorToWorkloadCR converts a workload.yaml descriptor to a Workload CR
func ConvertWorkloadDescriptorToWorkloadCR(descriptorPath string, params CreateWorkloadParams) (*openchoreov1alpha1.Workload, error) {
	conversion, err := ConvertWorkloadDescriptor(descriptorPath, params)
	if err != nil {
		return nil, err
	}
	return conversion.Workload, nil
}

// ConvertWorkloadDescriptor converts a workload.yaml descriptor to a Workload CR and, for a
// v1alpha2 descriptor, the Endpoints it asks for.
func ConvertWorkloadDescriptor(descriptorPath string, params CreateWorkloadParams) (*DescriptorConversion, error) {
	// Read the workload descriptor file
	descriptor, err := readWorkloadDescriptor(descriptorPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read workload descriptor: %w", err)
	}

	warnings, err := validateDescriptor(descriptor)
	if err != nil {
		return nil, fmt.Errorf("invalid workload descriptor: %w", err)
	}

	// Validate conversion parameters
	if err := validateConversionParams(params); err != nil {
		return nil, fmt.Errorf("invalid conversion parameters: %w", err)
	}

	// Convert descriptor to Workload CR with the base directory for resolving relative paths
	workload, err := convertDescriptorToWorkload(descriptor, params, descriptorPath)
	if err != nil {
		return nil, fmt.Errorf("failed to convert descriptor to workload CR: %w", err)
	}

	return &DescriptorConversion{
		Workload:  workload,
		Exposures: exposuresFromDescriptor(descriptor),
		Warnings:  warnings,
	}, nil
}

// readDescriptorFile reads relPath resolved against baseDir. It uses os.Root so a
// descriptor only references files within its own directory: a relPath that resolves
// outside baseDir — via "..", an absolute path, or a symlink pointing outside it —
// returns an error instead of reading the target.
func readDescriptorFile(baseDir, relPath string) ([]byte, error) {
	root, err := os.OpenRoot(baseDir)
	if err != nil {
		return nil, err
	}
	defer root.Close()

	f, err := root.Open(relPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	return io.ReadAll(f)
}

// readSchemaFile reads an endpoint schema file named relative to the descriptor
// directory (baseDir) via readDescriptorFile.
func readSchemaFile(baseDir, relPath string) (string, error) {
	content, err := readDescriptorFile(baseDir, relPath)
	if err != nil {
		return "", fmt.Errorf("failed to read schema file %s: %w", relPath, err)
	}
	return string(content), nil
}

func readWorkloadDescriptor(path string) (*WorkloadDescriptor, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open file %s: %w", path, err)
	}
	defer file.Close()

	return readWorkloadDescriptorFromReader(file)
}

// readWorkloadDescriptorFromReader decodes a descriptor by its apiVersion. v1alpha1 is decoded
// leniently, as it always was. v1alpha2 is decoded strictly, so a misspelled or unsupported key
// fails rather than being dropped: a dropped expose would silently leave an endpoint unexposed
// or exposed through the default EndpointType.
func readWorkloadDescriptorFromReader(reader io.Reader) (*WorkloadDescriptor, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to read data: %w", err)
	}

	var probe struct {
		APIVersion string `json:"apiVersion"`
	}
	if err := yaml.Unmarshal(data, &probe); err != nil {
		return nil, fmt.Errorf("failed to decode YAML: %w", err)
	}

	var descriptor WorkloadDescriptor
	switch probe.APIVersion {
	case "", DescriptorAPIVersionV1alpha1:
		if err := yaml.Unmarshal(data, &descriptor); err != nil {
			return nil, fmt.Errorf("failed to decode YAML: %w", err)
		}
	case DescriptorAPIVersionV1alpha2:
		if err := yaml.UnmarshalStrict(data, &descriptor); err != nil {
			return nil, fmt.Errorf("failed to decode %s descriptor: %w", DescriptorAPIVersionV1alpha2, err)
		}
	default:
		return nil, fmt.Errorf("unsupported descriptor apiVersion %q: must be %s or %s",
			probe.APIVersion, DescriptorAPIVersionV1alpha1, DescriptorAPIVersionV1alpha2)
	}

	return &descriptor, nil
}

// isV1alpha2 reports whether a descriptor uses the v1alpha2 format.
func (d *WorkloadDescriptor) isV1alpha2() bool {
	return d.APIVersion == DescriptorAPIVersionV1alpha2
}

// exposeNamePattern bounds an expose entry's name to a DNS label, since it becomes part of the
// Endpoint's name.
var exposeNamePattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

// validEndpointExposeVisibilities is the set of visibilities an Endpoint can request. project is
// implicit for every workload endpoint and never requested through an Endpoint.
var validEndpointExposeVisibilities = map[openchoreov1alpha1.EndpointVisibility]bool{
	openchoreov1alpha1.EndpointVisibilityNamespace: true,
	openchoreov1alpha1.EndpointVisibilityInternal:  true,
	openchoreov1alpha1.EndpointVisibilityExternal:  true,
}

// validateDescriptor applies the rules of the descriptor's version to its endpoints and returns
// warnings for deprecated fields. Field values shared by both versions (visibility values,
// dependencies, configurations) are checked during conversion.
func validateDescriptor(descriptor *WorkloadDescriptor) ([]string, error) {
	if !descriptor.isV1alpha2() {
		var warnings []string
		for _, ep := range descriptor.Endpoints {
			if len(ep.Expose) > 0 {
				return nil, fmt.Errorf("endpoint %q: expose requires apiVersion %s", ep.Name, DescriptorAPIVersionV1alpha2)
			}
			if ep.Type != "" {
				warnings = append(warnings, fmt.Sprintf("endpoint %q: type is deprecated, use protocol", ep.Name))
			}
			if len(ep.Visibility) > 0 {
				warnings = append(warnings, fmt.Sprintf(
					"endpoint %q: visibility is deprecated; expose the endpoint with expose in apiVersion %s",
					ep.Name, DescriptorAPIVersionV1alpha2))
			}
		}
		return warnings, nil
	}

	seen := make(map[string]bool, len(descriptor.Endpoints))
	for _, ep := range descriptor.Endpoints {
		if ep.Name == "" {
			return nil, fmt.Errorf("every endpoint needs a name")
		}
		if seen[ep.Name] {
			return nil, fmt.Errorf("endpoint %q is declared more than once", ep.Name)
		}
		seen[ep.Name] = true
		if ep.Protocol == "" {
			return nil, fmt.Errorf("endpoint %q: protocol is required", ep.Name)
		}
		if ep.Type != "" {
			return nil, fmt.Errorf("endpoint %q: type is not supported in %s, use protocol", ep.Name, DescriptorAPIVersionV1alpha2)
		}
		if len(ep.Visibility) > 0 {
			return nil, fmt.Errorf("endpoint %q: visibility is not supported in %s, use expose", ep.Name, DescriptorAPIVersionV1alpha2)
		}
		if err := validateExpose(ep); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

// validateExpose checks a v1alpha2 endpoint's expose entries. Whether via names an allowed
// EndpointType and whether the parameters fit it is decided by the server, which knows the
// component's ComponentType.
//
// At most one entry may be unnamed. Its Endpoint takes the endpoint's own name, so adding a
// named entry later does not rename it.
func validateExpose(ep WorkloadDescriptorEndpoint) error {
	names := make(map[string]bool, len(ep.Expose))
	for i, ex := range ep.Expose {
		if ex.Name != "" && (!exposeNamePattern.MatchString(ex.Name) || len(ex.Name) > 63) {
			return fmt.Errorf("endpoint %q: expose[%d] name %q must be a lowercase DNS label", ep.Name, i, ex.Name)
		}
		if names[ex.Name] {
			if ex.Name == "" {
				return fmt.Errorf("endpoint %q: expose[%d] needs a name: only one expose entry of an endpoint may be unnamed", ep.Name, i)
			}
			return fmt.Errorf("endpoint %q: expose name %q is used more than once", ep.Name, ex.Name)
		}
		names[ex.Name] = true
		if len(ex.Visibility) == 0 {
			return fmt.Errorf("endpoint %q: expose[%d] needs at least one visibility", ep.Name, i)
		}
		visibilities := make(map[string]bool, len(ex.Visibility))
		for _, v := range ex.Visibility {
			if !validEndpointExposeVisibilities[openchoreov1alpha1.EndpointVisibility(v)] {
				return fmt.Errorf("endpoint %q: expose[%d] visibility %q must be one of [namespace, internal, external]",
					ep.Name, i, v)
			}
			if visibilities[v] {
				return fmt.Errorf("endpoint %q: expose[%d] lists visibility %q more than once", ep.Name, i, v)
			}
			visibilities[v] = true
		}
	}
	return nil
}

// exposuresFromDescriptor lists a v1alpha2 descriptor's expose entries in declaration order. It
// returns nil for v1alpha1, which cannot express exposure, and an empty list for a v1alpha2
// descriptor that exposes nothing.
func exposuresFromDescriptor(descriptor *WorkloadDescriptor) []WorkloadExposure {
	if !descriptor.isV1alpha2() {
		return nil
	}
	exposures := []WorkloadExposure{}
	for _, ep := range descriptor.Endpoints {
		for _, ex := range ep.Expose {
			exposures = append(exposures, WorkloadExposure{
				Endpoint:   ep.Name,
				Name:       ex.Name,
				Visibility: ex.Visibility,
				Via:        ex.Via,
				Parameters: ex.Parameters,
			})
		}
	}
	return exposures
}

func validateConversionParams(params CreateWorkloadParams) error {
	if params.NamespaceName == "" {
		return fmt.Errorf("namespace name is required")
	}
	if params.ProjectName == "" {
		return fmt.Errorf("project name is required")
	}
	if params.ComponentName == "" {
		return fmt.Errorf("component name is required")
	}
	if params.ImageURL == "" {
		return fmt.Errorf("image URL is required")
	}
	return nil
}

// WorkloadName names the Workload of a component's release line: "{component}-workload" for its
// main line and "{componentLineName}-workload" for a release line.
func WorkloadName(componentName, releaseLine string) string {
	return releaseline.ScopedName(componentName, releaseline.Normalize(releaseLine), "workload")
}

// createBaseWorkload creates the basic workload structure with common fields
func createBaseWorkload(workloadName string, params CreateWorkloadParams) (*openchoreov1alpha1.Workload, error) {
	source, err := SourceFromParams(params)
	if err != nil {
		return nil, err
	}

	workload := &openchoreov1alpha1.Workload{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "openchoreo.dev/v1alpha1",
			Kind:       "Workload",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      workloadName,
			Namespace: params.NamespaceName,
		},
		Spec: openchoreov1alpha1.WorkloadSpec{
			Owner: openchoreov1alpha1.WorkloadOwner{
				ProjectName:   params.ProjectName,
				ComponentName: params.ComponentName,
				ReleaseLine:   releaseline.Normalize(params.ReleaseLine),
			},
			WorkloadTemplateSpec: openchoreov1alpha1.WorkloadTemplateSpec{
				Container: openchoreov1alpha1.Container{
					Image: params.ImageURL,
				},
				Source: source,
			},
		},
	}

	return workload, nil
}

// SourceFromParams builds the workload's commit provenance from CLI flags, or
// nil when none were given (Source is optional: DF/CFR/MTTR compute without
// it; only Lead Time for Changes needs it).
func SourceFromParams(params CreateWorkloadParams) (*openchoreov1alpha1.WorkloadSource, error) {
	if params.SourceCommit == "" && params.SourceBranch == "" &&
		params.SourceRepository == "" && params.SourceAuthoredAt == "" {
		return nil, nil
	}

	source := &openchoreov1alpha1.WorkloadSource{
		Commit:     params.SourceCommit,
		Branch:     params.SourceBranch,
		Repository: params.SourceRepository,
	}
	if params.SourceAuthoredAt != "" {
		authoredAt, err := time.Parse(time.RFC3339, params.SourceAuthoredAt)
		if err != nil {
			return nil, fmt.Errorf("invalid --source-authored-at %q: must be RFC3339 (e.g. from "+
				"'git show -s --format=%%aI <sha>'): %w", params.SourceAuthoredAt, err)
		}
		metaTime := metav1.NewTime(authoredAt)
		source.AuthoredAt = &metaTime
	}
	return source, nil
}

func convertDescriptorToWorkload(descriptor *WorkloadDescriptor, params CreateWorkloadParams, descriptorPath string) (*openchoreov1alpha1.Workload, error) {
	// Determine workload name
	workloadName := WorkloadName(params.ComponentName, params.ReleaseLine)
	if workloadName == "" {
		return nil, fmt.Errorf("workload name must be provided either in params or descriptor metadata")
	}

	// Create the base workload structure
	workload, err := createBaseWorkload(workloadName, params)
	if err != nil {
		return nil, err
	}

	// Add endpoints from descriptor if present
	if err := addEndpointsFromDescriptor(workload, descriptor, descriptorPath); err != nil {
		return nil, fmt.Errorf("failed to add endpoints: %w", err)
	}

	// Add dependencies from descriptor if present
	if err := addDependenciesFromDescriptor(workload, descriptor); err != nil {
		return nil, fmt.Errorf("failed to add dependencies: %w", err)
	}

	// Add configurations from descriptor if present
	if err := addConfigurationsFromDescriptor(workload, descriptor, descriptorPath); err != nil {
		return nil, fmt.Errorf("failed to add configurations: %w", err)
	}

	return workload, nil
}

// addEndpointsFromDescriptor adds endpoints from the descriptor to the workload
func addEndpointsFromDescriptor(workload *openchoreov1alpha1.Workload, descriptor *WorkloadDescriptor, descriptorPath string) error {
	if len(descriptor.Endpoints) == 0 {
		return nil
	}

	workload.Spec.Endpoints = make(map[string]openchoreov1alpha1.WorkloadEndpoint)
	for _, descriptorEndpoint := range descriptor.Endpoints {
		var visibility []openchoreov1alpha1.EndpointVisibility
		for _, v := range descriptorEndpoint.Visibility {
			vis := openchoreov1alpha1.EndpointVisibility(v)
			if !validEndpointVisibilities[vis] {
				return fmt.Errorf("invalid endpoint visibility %q for endpoint %q: must be one of [project, namespace, internal, external]",
					v, descriptorEndpoint.Name)
			}
			visibility = append(visibility, vis)
		}

		protocol, err := descriptorEndpointProtocol(descriptorEndpoint)
		if err != nil {
			return err
		}

		// Both protocol and its deprecated name type are written: a control plane that predates
		// protocol prunes it and still requires type.
		endpoint := openchoreov1alpha1.WorkloadEndpoint{
			DisplayName: descriptorEndpoint.DisplayName,
			Port:        descriptorEndpoint.Port,
			TargetPort:  descriptorEndpoint.TargetPort,
			Protocol:    protocol,
			Type:        protocol, //nolint:staticcheck // deprecated field still written for older control planes
			BasePath:    descriptorEndpoint.BasePath,
			Visibility:  visibility,
		}

		// Set the schema only when a schema file is provided. The schema type is
		// derived from the endpoint protocol (e.g. HTTP -> openapi, gRPC -> proto)
		// rather than copied from the endpoint type verbatim, so it matches the
		// canonical formats understood by the rendering pipeline's schema extractor.
		if descriptorEndpoint.SchemaFile != "" {
			// Resolve the schema file relative to the workload descriptor directory.
			baseDir := filepath.Dir(descriptorPath)

			// Read schema file content and inline it
			schemaContent, err := readSchemaFile(baseDir, descriptorEndpoint.SchemaFile)
			if err != nil {
				return err
			}

			endpoint.Schema = &openchoreov1alpha1.Schema{
				Type:    schemaFormatByEndpointType[protocol],
				Content: schemaContent,
			}
		}

		workload.Spec.Endpoints[descriptorEndpoint.Name] = endpoint
	}
	return nil
}

// descriptorEndpointProtocol reads an endpoint's protocol from protocol or its deprecated name
// type. Setting both is allowed only when they agree.
func descriptorEndpointProtocol(ep WorkloadDescriptorEndpoint) (openchoreov1alpha1.EndpointProtocol, error) {
	switch {
	case ep.Protocol != "" && ep.Type != "" && ep.Protocol != ep.Type:
		return "", fmt.Errorf("endpoint %q: type %q does not match protocol %q; set only protocol", ep.Name, ep.Type, ep.Protocol)
	case ep.Protocol != "":
		return openchoreov1alpha1.EndpointProtocol(ep.Protocol), nil
	case ep.Type != "":
		return openchoreov1alpha1.EndpointProtocol(ep.Type), nil
	default:
		return "", fmt.Errorf("endpoint %q: protocol is required", ep.Name)
	}
}

// schemaFormatByEndpointType maps an endpoint protocol to the canonical schema
// format used for its API definition. These values mirror the canonical schema
// types recognized by internal/pipeline/component/schemaextract. Endpoint types
// with no API schema format (TCP, UDP, Websocket) are intentionally absent, so a
// map lookup yields "" and no schema type is emitted for them.
var schemaFormatByEndpointType = map[openchoreov1alpha1.EndpointProtocol]string{
	openchoreov1alpha1.EndpointProtocolHTTP:    "openapi",
	openchoreov1alpha1.EndpointProtocolGRPC:    "proto",
	openchoreov1alpha1.EndpointProtocolGraphQL: "graphql",
}

// validEndpointVisibilities is the set of allowed visibility values for endpoints.
var validEndpointVisibilities = map[openchoreov1alpha1.EndpointVisibility]bool{
	openchoreov1alpha1.EndpointVisibilityProject:   true,
	openchoreov1alpha1.EndpointVisibilityNamespace: true,
	openchoreov1alpha1.EndpointVisibilityInternal:  true,
	openchoreov1alpha1.EndpointVisibilityExternal:  true,
}

// validDependencyVisibilities is the set of allowed visibility values for dependency endpoint connections.
// WorkloadConnection.Visibility is restricted to project and namespace by the CRD validation.
var validDependencyVisibilities = map[openchoreov1alpha1.EndpointVisibility]bool{
	openchoreov1alpha1.EndpointVisibilityProject:   true,
	openchoreov1alpha1.EndpointVisibilityNamespace: true,
}

// addDependenciesFromDescriptor adds dependencies from the descriptor to the workload
func addDependenciesFromDescriptor(workload *openchoreov1alpha1.Workload, descriptor *WorkloadDescriptor) error {
	if descriptor.Dependencies == nil ||
		(len(descriptor.Dependencies.Endpoints) == 0 && len(descriptor.Dependencies.Resources) == 0) {
		return nil
	}

	connections, err := buildEndpointConnections(descriptor.Dependencies.Endpoints)
	if err != nil {
		return err
	}
	resources, err := buildResourceDependencies(descriptor.Dependencies.Resources)
	if err != nil {
		return err
	}

	if len(connections) == 0 && len(resources) == 0 {
		return nil
	}

	if workload.Spec.Dependencies == nil {
		workload.Spec.Dependencies = &openchoreov1alpha1.WorkloadDependencies{}
	}
	workload.Spec.Dependencies.Endpoints = connections
	workload.Spec.Dependencies.Resources = resources
	return nil
}

func buildEndpointConnections(entries []WorkloadDescriptorConnection) ([]openchoreov1alpha1.WorkloadConnection, error) {
	if len(entries) == 0 {
		return nil, nil
	}
	connections := make([]openchoreov1alpha1.WorkloadConnection, 0, len(entries))
	for i, dc := range entries {
		if dc.Component == "" {
			return nil, fmt.Errorf("dependency endpoint[%d]: component is required", i)
		}
		if dc.Name == "" {
			return nil, fmt.Errorf("dependency endpoint[%d] (component %q): name is required", i, dc.Component)
		}
		if dc.Visibility == "" {
			return nil, fmt.Errorf("dependency endpoint[%d] (component %q): visibility is required", i, dc.Component)
		}
		visibility := openchoreov1alpha1.EndpointVisibility(dc.Visibility)
		if !validDependencyVisibilities[visibility] {
			return nil, fmt.Errorf("invalid dependency endpoint visibility %q for component %q endpoint %q: must be one of [project, namespace]",
				dc.Visibility, dc.Component, dc.Name)
		}
		connections = append(connections, openchoreov1alpha1.WorkloadConnection{
			Project:    dc.Project,
			Component:  dc.Component,
			Name:       dc.Name,
			Visibility: string(visibility),
			EnvBindings: openchoreov1alpha1.ConnectionEnvBindings{
				Address:  dc.EnvBindings.Address,
				Host:     dc.EnvBindings.Host,
				Port:     dc.EnvBindings.Port,
				BasePath: dc.EnvBindings.BasePath,
			},
		})
	}
	return connections, nil
}

func buildResourceDependencies(entries []WorkloadDescriptorResourceDependency) ([]openchoreov1alpha1.WorkloadResourceDependency, error) {
	if len(entries) == 0 {
		return nil, nil
	}
	resources := make([]openchoreov1alpha1.WorkloadResourceDependency, 0, len(entries))
	for i, dr := range entries {
		if dr.Ref == "" {
			return nil, fmt.Errorf("dependency resource[%d]: ref is required", i)
		}
		for k, v := range dr.EnvBindings {
			if k == "" || v == "" {
				return nil, fmt.Errorf("dependency resource[%d] (ref %q): envBindings keys (output names) and values (env var names) cannot be empty", i, dr.Ref)
			}
		}
		for k, v := range dr.FileBindings {
			if k == "" || v == "" {
				return nil, fmt.Errorf("dependency resource[%d] (ref %q): fileBindings keys (output names) and values (mount paths) cannot be empty", i, dr.Ref)
			}
		}
		resources = append(resources, openchoreov1alpha1.WorkloadResourceDependency{
			Ref:          dr.Ref,
			EnvBindings:  dr.EnvBindings,
			FileBindings: dr.FileBindings,
		})
	}
	return resources, nil
}

// addConfigurationsFromDescriptor adds configurations (env vars and files) from the descriptor to the workload
func addConfigurationsFromDescriptor(workload *openchoreov1alpha1.Workload, descriptor *WorkloadDescriptor, descriptorPath string) error {
	// Add environment variables
	if len(descriptor.Configurations.Env) > 0 {
		workload.Spec.Container.Env = make([]openchoreov1alpha1.EnvVar, len(descriptor.Configurations.Env))
		for i, envVar := range descriptor.Configurations.Env {
			crEnvVar := openchoreov1alpha1.EnvVar{
				Key: envVar.Name,
			}

			if envVar.ValueFrom != nil && envVar.ValueFrom.SecretKeyRef != nil {
				crEnvVar.ValueFrom = convertEnvVarSource(envVar.ValueFrom)
			} else if envVar.Value != "" {
				crEnvVar.Value = envVar.Value
			}

			workload.Spec.Container.Env[i] = crEnvVar
		}
	}

	// Add file configurations
	if len(descriptor.Configurations.Files) > 0 {
		workload.Spec.Container.Files = make([]openchoreov1alpha1.FileVar, 0, len(descriptor.Configurations.Files))
		baseDir := filepath.Dir(descriptorPath)

		for _, fileVar := range descriptor.Configurations.Files {
			crFileVar := openchoreov1alpha1.FileVar{
				Key:       fileVar.Name,
				MountPath: fileVar.MountPath,
			}

			// Only set value OR valueFrom, never both
			// Priority: SecretKeyRef > Path > inline Value
			if fileVar.ValueFrom != nil && fileVar.ValueFrom.SecretKeyRef != nil {
				// Reference to secret
				crFileVar.ValueFrom = convertEnvVarSource(fileVar.ValueFrom)
			} else if fileVar.ValueFrom != nil && fileVar.ValueFrom.Path != "" {
				// Read file content, resolved relative to the descriptor directory.
				content, err := readDescriptorFile(baseDir, fileVar.ValueFrom.Path)
				if err != nil {
					return fmt.Errorf("failed to read file %s: %w", fileVar.ValueFrom.Path, err)
				}
				crFileVar.Value = string(content)
			} else if fileVar.Value != "" {
				// Inline value
				crFileVar.Value = fileVar.Value
			}

			workload.Spec.Container.Files = append(workload.Spec.Container.Files, crFileVar)
		}
	}

	return nil
}

// convertEnvVarSource converts a descriptor env var source to a CR env var source
func convertEnvVarSource(source *WorkloadDescriptorEnvVarSource) *openchoreov1alpha1.EnvVarValueFrom {
	if source == nil {
		return nil
	}

	result := &openchoreov1alpha1.EnvVarValueFrom{}

	if source.SecretKeyRef != nil {
		result.SecretKeyRef = &openchoreov1alpha1.SecretKeyRef{
			Name: source.SecretKeyRef.Name,
			Key:  source.SecretKeyRef.Key,
		}
	}

	return result
}

// CreateBasicWorkload creates a basic Workload CR without reading from a descriptor file
func CreateBasicWorkload(params CreateWorkloadParams) (*openchoreov1alpha1.Workload, error) {
	// Validate conversion parameters
	if err := validateConversionParams(params); err != nil {
		return nil, fmt.Errorf("invalid conversion parameters: %w", err)
	}

	// Generate workload name from component name and release line
	workloadName := WorkloadName(params.ComponentName, params.ReleaseLine)

	// Create the basic workload using shared function
	workload, err := createBaseWorkload(workloadName, params)
	if err != nil {
		return nil, err
	}

	return workload, nil
}

// ConvertWorkloadCRToYAML converts a Workload CR to clean YAML bytes with proper field ordering
func ConvertWorkloadCRToYAML(workload *openchoreov1alpha1.Workload) ([]byte, error) {
	// Create a custom structure to control field ordering
	// Note: sigs.k8s.io/yaml uses JSON tags, but we keep both for compatibility
	type orderedWorkload struct {
		APIVersion string `json:"apiVersion" yaml:"apiVersion"`
		Kind       string `json:"kind" yaml:"kind"`
		Metadata   struct {
			Name      string `json:"name" yaml:"name"`
			Namespace string `json:"namespace,omitempty" yaml:"namespace,omitempty"`
		} `json:"metadata" yaml:"metadata"`
		Spec struct {
			Owner        openchoreov1alpha1.WorkloadOwner               `json:"owner" yaml:"owner"`
			Container    openchoreov1alpha1.Container                   `json:"container" yaml:"container"`
			Endpoints    map[string]openchoreov1alpha1.WorkloadEndpoint `json:"endpoints,omitempty" yaml:"endpoints,omitempty"`
			Dependencies *openchoreov1alpha1.WorkloadDependencies       `json:"dependencies,omitempty" yaml:"dependencies,omitempty"`
			Source       *openchoreov1alpha1.WorkloadSource             `json:"source,omitempty" yaml:"source,omitempty"`
		} `json:"spec" yaml:"spec"`
	}

	// Create the ordered structure
	ordered := orderedWorkload{
		APIVersion: workload.APIVersion,
		Kind:       workload.Kind,
	}
	ordered.Metadata.Name = workload.Name
	ordered.Metadata.Namespace = workload.Namespace
	ordered.Spec.Owner = workload.Spec.Owner
	ordered.Spec.Container = workload.Spec.Container
	ordered.Spec.Endpoints = workload.Spec.Endpoints
	ordered.Spec.Dependencies = workload.Spec.Dependencies
	ordered.Spec.Source = workload.Spec.Source

	// Marshal with sigs.k8s.io/yaml for JSON tag support
	return yaml.Marshal(ordered)
}
