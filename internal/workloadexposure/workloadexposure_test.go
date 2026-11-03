// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package workloadexposure

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/openchoreo/openchoreo/api/v1alpha1"
	"github.com/openchoreo/openchoreo/internal/labels"
)

// The expected values come from the portal's toDnsLabel, so both clients name Endpoints alike.
func TestToDNSLabelMatchesPortal(t *testing.T) {
	tests := map[string]string{
		"orders-api":    "orders-api",
		"Orders_API.v1": "orders-api-v1",
		"--x__y--":      "x-y",
		strings.Repeat("a", 50) + "-" + strings.Repeat("b", 30): "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-bbb-94716bda",
		"Ünïcode-Ä" + strings.Repeat("z", 70):                   "n-code-zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz-933a3358",
	}
	for in, want := range tests {
		assert.Equal(t, want, ToDNSLabel(in), in)
	}
}

func TestEndpointName(t *testing.T) {
	assert.Equal(t, "orders-api", EndpointName("orders", "", "api", ""))
	assert.Equal(t, "orders-api-internal", EndpointName("orders", "", "api", "internal"))
	assert.Equal(t, "orders-next-api", EndpointName("orders", "next", "api", ""))
	assert.Equal(t, "orders-next-api-internal", EndpointName("orders", "next", "api", "internal"))
}

type fakeLookup struct {
	allowed []v1alpha1.AllowedEndpointType
	schemas map[string]*v1alpha1.SchemaSection
}

func (f fakeLookup) AllowedEndpoints(context.Context, string, v1alpha1.ComponentTypeRef) ([]v1alpha1.AllowedEndpointType, error) {
	return f.allowed, nil
}

func (f fakeLookup) EndpointTypeParameters(_ context.Context, _ string, ref v1alpha1.EndpointTypeRef) (*v1alpha1.SchemaSection, error) {
	return f.schemas[ref.Name], nil
}

func clusterType(name string) v1alpha1.EndpointTypeRef {
	return v1alpha1.EndpointTypeRef{Kind: v1alpha1.EndpointTypeRefKindClusterEndpointType, Name: name}
}

func testLookup() fakeLookup {
	return fakeLookup{
		allowed: []v1alpha1.AllowedEndpointType{
			{Name: "web", Protocols: []v1alpha1.EndpointProtocol{v1alpha1.EndpointProtocolHTTP}, Ref: clusterType("http-route"), Default: true},
			{Name: "managed-api", Protocols: []v1alpha1.EndpointProtocol{v1alpha1.EndpointProtocolHTTP}, Ref: clusterType("managed-http")},
			{Name: "grpc", Protocols: []v1alpha1.EndpointProtocol{v1alpha1.EndpointProtocolGRPC}, Ref: clusterType("grpc-route")},
		},
		schemas: map[string]*v1alpha1.SchemaSection{
			"managed-http": {OpenAPIV3Schema: &runtime.RawExtension{Raw: []byte(
				`{"type":"object","properties":{"requireApiKey":{"type":"boolean","default":false}},"additionalProperties":false}`)}},
		},
	}
}

func testTarget() Target {
	return Target{
		Namespace:     "default",
		Project:       "shop",
		Component:     "greeter",
		ComponentType: v1alpha1.ComponentTypeRef{Kind: v1alpha1.ComponentTypeRefKindClusterComponentType, Name: "deployment/service"},
		AutoDeploy:    true,
		Endpoints: map[string]v1alpha1.WorkloadEndpoint{
			"public-api": {Port: 9090, Protocol: v1alpha1.EndpointProtocolHTTP},
			"rpc":        {Port: 9091, Type: v1alpha1.EndpointProtocolGRPC}, //nolint:staticcheck // a workload stored before protocol existed
		},
	}
}

func external() []v1alpha1.EndpointVisibility {
	return []v1alpha1.EndpointVisibility{v1alpha1.EndpointVisibilityExternal}
}

func TestGenerate(t *testing.T) {
	ctx := context.Background()

	t.Run("default and aliased entries", func(t *testing.T) {
		endpoints, err := Generate(ctx, testTarget(), []Exposure{
			{Endpoint: "public-api", Visibility: external(), Via: "managed-api",
				Parameters: &runtime.RawExtension{Raw: []byte(`{"requireApiKey":true}`)}},
			{Endpoint: "public-api", Name: "internal", Visibility: []v1alpha1.EndpointVisibility{v1alpha1.EndpointVisibilityInternal}},
		}, testLookup())
		require.NoError(t, err)
		require.Len(t, endpoints, 2)

		managed := endpoints[0]
		assert.Equal(t, "greeter-public-api", managed.Name)
		assert.Equal(t, clusterType("managed-http"), managed.Spec.Type)
		assert.JSONEq(t, `{"requireApiKey":true}`, string(managed.Spec.Parameters.Raw))
		assert.True(t, managed.Spec.AutoDeploy)
		assert.Equal(t, map[string]string{
			labels.LabelKeyProjectName:     "shop",
			labels.LabelKeySourceComponent: "greeter",
			labels.LabelKeySourceEndpoint:  "public-api",
			labels.LabelKeyManagedBy:       ManagedByWorkloadDescriptor,
		}, managed.Labels)
		require.Len(t, managed.Spec.BackendRefs, 1)
		assert.Equal(t, "greeter", managed.Spec.BackendRefs[0].Name)
		assert.Equal(t, &v1alpha1.EndpointComponentBackend{Name: "greeter", Endpoint: "public-api"}, managed.Spec.BackendRefs[0].Component)

		internal := endpoints[1]
		assert.Equal(t, "greeter-public-api-internal", internal.Name)
		assert.Equal(t, clusterType("http-route"), internal.Spec.Type)
		assert.Nil(t, internal.Spec.Parameters)
	})

	t.Run("release line", func(t *testing.T) {
		target := testTarget()
		target.ReleaseLine = "next"
		endpoints, err := Generate(ctx, target, []Exposure{{Endpoint: "public-api", Visibility: external()}}, testLookup())
		require.NoError(t, err)
		ep := endpoints[0]
		assert.Equal(t, "greeter-next-public-api", ep.Name)
		assert.Equal(t, "next", ep.Labels[labels.LabelKeySourceReleaseLine])
		assert.Equal(t, "next", ep.Spec.BackendRefs[0].Name)
		assert.Equal(t, "next", ep.Spec.BackendRefs[0].Component.ReleaseLine)
	})

	t.Run("no exposures", func(t *testing.T) {
		endpoints, err := Generate(ctx, testTarget(), nil, fakeLookup{})
		require.NoError(t, err)
		assert.Empty(t, endpoints)
	})

	invalidCases := []struct {
		name     string
		lookup   fakeLookup
		exposure Exposure
		wantErr  string
	}{
		{
			name:     "component type allows nothing",
			lookup:   fakeLookup{},
			exposure: Exposure{Endpoint: "public-api", Visibility: external()},
			wantErr:  "allows no Endpoint types",
		},
		{
			name:     "unknown workload endpoint",
			exposure: Exposure{Endpoint: "missing", Visibility: external()},
			wantErr:  `the workload has no endpoint "missing"`,
		},
		{
			name:     "unknown alias never falls back to the default",
			exposure: Exposure{Endpoint: "public-api", Visibility: external(), Via: "managed"},
			wantErr:  `via "managed" is not an allowed Endpoint type for protocol HTTP (available: web, managed-api)`,
		},
		{
			name:     "alias for another protocol",
			exposure: Exposure{Endpoint: "public-api", Visibility: external(), Via: "grpc"},
			wantErr:  "is not an allowed Endpoint type for protocol HTTP",
		},
		{
			name:     "no default for the protocol",
			exposure: Exposure{Endpoint: "rpc", Visibility: external()},
			wantErr:  "no default Endpoint type for protocol gRPC; name one with via (available: grpc)",
		},
		{
			name: "parameters outside the schema",
			exposure: Exposure{Endpoint: "public-api", Visibility: external(), Via: "managed-api",
				Parameters: &runtime.RawExtension{Raw: []byte(`{"requireApiKey":"yes"}`)}},
			wantErr: "parameters do not fit ClusterEndpointType managed-http",
		},
		{
			name:     "project visibility",
			exposure: Exposure{Endpoint: "public-api", Visibility: []v1alpha1.EndpointVisibility{v1alpha1.EndpointVisibilityProject}},
			wantErr:  "project visibility is implicit",
		},
	}
	for _, tt := range invalidCases {
		t.Run(tt.name, func(t *testing.T) {
			lookup := tt.lookup
			if lookup.allowed == nil && tt.name != "component type allows nothing" {
				lookup = testLookup()
			}
			_, err := Generate(ctx, testTarget(), []Exposure{tt.exposure}, lookup)
			var invalidErr *InvalidExposureError
			require.True(t, errors.As(err, &invalidErr), "want InvalidExposureError, got %v", err)
			assert.Contains(t, invalidErr.Msg, tt.wantErr)
		})
	}

	t.Run("two exposures with one name", func(t *testing.T) {
		_, err := Generate(ctx, testTarget(), []Exposure{
			{Endpoint: "public-api", Visibility: external()},
			{Endpoint: "public-api", Visibility: external()},
		}, testLookup())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "would both be named greeter-public-api")
	})
}
