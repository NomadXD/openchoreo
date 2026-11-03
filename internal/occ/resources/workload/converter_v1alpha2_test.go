// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package synth

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	openchoreov1alpha1 "github.com/openchoreo/openchoreo/api/v1alpha1"
	"github.com/openchoreo/openchoreo/internal/occ/testutil"
)

func convertDescriptorContent(t *testing.T, content string) (*DescriptorConversion, error) {
	t.Helper()
	dir := t.TempDir()
	testutil.WriteYAML(t, dir, "workload.yaml", content)
	return ConvertWorkloadDescriptor(filepath.Join(dir, "workload.yaml"), CreateWorkloadParams{
		NamespaceName: "test-ns",
		ProjectName:   "test-project",
		ComponentName: "greeter",
		ImageURL:      "gcr.io/img:v1",
	})
}

func TestConvertWorkloadDescriptorV1alpha2(t *testing.T) {
	conversion, err := convertDescriptorContent(t, `apiVersion: openchoreo.dev/v1alpha2
metadata:
  name: greeter
endpoints:
  - name: public-api
    protocol: HTTP
    port: 9090
    basePath: /greeter
    expose:
      - visibility: [external]
        via: managed-api
        parameters:
          requireApiKey: true
      - name: internal
        visibility: [internal]
  - name: metrics
    protocol: HTTP
    port: 9100
`)
	require.NoError(t, err)
	assert.Empty(t, conversion.Warnings)

	ep := conversion.Workload.Spec.Endpoints["public-api"]
	assert.Equal(t, openchoreov1alpha1.EndpointProtocolHTTP, ep.Protocol)
	assert.Equal(t, openchoreov1alpha1.EndpointProtocolHTTP, ep.Type)                          //nolint:staticcheck // written for older control planes
	assert.Empty(t, ep.Visibility, "v1alpha2 never writes the deprecated workload visibility") //nolint:staticcheck // asserting the deprecated field stays unset
	assert.Contains(t, conversion.Workload.Spec.Endpoints, "metrics")

	assert.Equal(t, []WorkloadExposure{
		{
			Endpoint:   "public-api",
			Visibility: []string{"external"},
			Via:        "managed-api",
			Parameters: map[string]any{"requireApiKey": true},
		},
		{Endpoint: "public-api", Name: "internal", Visibility: []string{"internal"}},
	}, conversion.Exposures)
}

func TestConvertWorkloadDescriptorV1alpha2ExposesNothing(t *testing.T) {
	conversion, err := convertDescriptorContent(t, `apiVersion: openchoreo.dev/v1alpha2
metadata:
  name: greeter
endpoints:
  - name: http
    protocol: HTTP
    port: 8080
`)
	require.NoError(t, err)
	require.NotNil(t, conversion.Exposures, "v1alpha2 with no expose asks for descriptor-managed Endpoints to be removed")
	assert.Empty(t, conversion.Exposures)
}

func TestConvertWorkloadDescriptorV1alpha2Rejects(t *testing.T) {
	tests := []struct {
		name      string
		endpoints string
		wantErr   string
	}{
		{
			name: "unknown key",
			endpoints: `  - name: http
    protocol: HTTP
    port: 8080
    exposure: managed-api`,
			wantErr: "unknown field",
		},
		{
			name: "missing protocol",
			endpoints: `  - name: http
    port: 8080`,
			wantErr: "protocol is required",
		},
		{
			name: "deprecated type",
			endpoints: `  - name: http
    protocol: HTTP
    type: HTTP
    port: 8080`,
			wantErr: "type is not supported",
		},
		{
			name: "deprecated visibility",
			endpoints: `  - name: http
    protocol: HTTP
    port: 8080
    visibility: [external]`,
			wantErr: "use expose",
		},
		{
			name: "duplicate endpoint",
			endpoints: `  - name: http
    protocol: HTTP
    port: 8080
  - name: http
    protocol: HTTP
    port: 8081`,
			wantErr: "declared more than once",
		},
		{
			name: "unnamed entry among several",
			endpoints: `  - name: http
    protocol: HTTP
    port: 8080
    expose:
      - visibility: [external]
      - visibility: [internal]`,
			wantErr: "needs a name",
		},
		{
			name: "duplicate expose name",
			endpoints: `  - name: http
    protocol: HTTP
    port: 8080
    expose:
      - name: a
        visibility: [external]
      - name: a
        visibility: [internal]`,
			wantErr: "used more than once",
		},
		{
			name: "expose name not a DNS label",
			endpoints: `  - name: http
    protocol: HTTP
    port: 8080
    expose:
      - name: Public_API
        visibility: [external]`,
			wantErr: "lowercase DNS label",
		},
		{
			name: "no visibility",
			endpoints: `  - name: http
    protocol: HTTP
    port: 8080
    expose:
      - via: managed-api`,
			wantErr: "at least one visibility",
		},
		{
			name: "project visibility",
			endpoints: `  - name: http
    protocol: HTTP
    port: 8080
    expose:
      - visibility: [project]`,
			wantErr: "must be one of [namespace, internal, external]",
		},
		{
			name: "repeated visibility",
			endpoints: `  - name: http
    protocol: HTTP
    port: 8080
    expose:
      - visibility: [external, external]`,
			wantErr: "more than once",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := convertDescriptorContent(t, "apiVersion: openchoreo.dev/v1alpha2\nmetadata:\n  name: greeter\nendpoints:\n"+tt.endpoints+"\n")
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestConvertWorkloadDescriptorV1alpha1Protocol(t *testing.T) {
	tests := []struct {
		name         string
		endpoint     string
		wantProtocol openchoreov1alpha1.EndpointProtocol
		wantWarnings []string
		wantErr      string
	}{
		{
			name:         "protocol only",
			endpoint:     "protocol: gRPC",
			wantProtocol: openchoreov1alpha1.EndpointProtocolGRPC,
		},
		{
			name:         "deprecated type only",
			endpoint:     "type: HTTP",
			wantProtocol: openchoreov1alpha1.EndpointProtocolHTTP,
			wantWarnings: []string{`endpoint "http": type is deprecated, use protocol`},
		},
		{
			name:         "both, equal",
			endpoint:     "protocol: HTTP\n    type: HTTP",
			wantProtocol: openchoreov1alpha1.EndpointProtocolHTTP,
			wantWarnings: []string{`endpoint "http": type is deprecated, use protocol`},
		},
		{
			name:     "both, different",
			endpoint: "protocol: HTTP\n    type: gRPC",
			wantErr:  "does not match protocol",
		},
		{
			name:     "neither",
			endpoint: "basePath: /",
			wantErr:  "protocol is required",
		},
		{
			name:     "expose needs v1alpha2",
			endpoint: "protocol: HTTP\n    expose:\n      - visibility: [external]",
			wantErr:  "expose requires apiVersion openchoreo.dev/v1alpha2",
		},
		{
			name:         "deprecated visibility",
			endpoint:     "protocol: HTTP\n    visibility: [external]",
			wantProtocol: openchoreov1alpha1.EndpointProtocolHTTP,
			wantWarnings: []string{`endpoint "http": visibility is deprecated; expose the endpoint with expose in apiVersion openchoreo.dev/v1alpha2`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conversion, err := convertDescriptorContent(t, `apiVersion: openchoreo.dev/v1alpha1
metadata:
  name: greeter
endpoints:
  - name: http
    port: 8080
    `+tt.endpoint+"\n")
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			ep := conversion.Workload.Spec.Endpoints["http"]
			assert.Equal(t, tt.wantProtocol, ep.Protocol)
			assert.Equal(t, tt.wantProtocol, ep.Type) //nolint:staticcheck // written for older control planes
			assert.Equal(t, tt.wantWarnings, conversion.Warnings)
			assert.Nil(t, conversion.Exposures, "v1alpha1 cannot express exposure")
		})
	}
}

func TestReadWorkloadDescriptorAPIVersion(t *testing.T) {
	t.Run("missing apiVersion reads as v1alpha1", func(t *testing.T) {
		conversion, err := convertDescriptorContent(t, "metadata:\n  name: greeter\n")
		require.NoError(t, err)
		assert.Nil(t, conversion.Exposures)
	})
	t.Run("v1alpha1 stays lenient about unknown keys", func(t *testing.T) {
		_, err := convertDescriptorContent(t, "apiVersion: openchoreo.dev/v1alpha1\nmetadata:\n  name: greeter\nsomethingElse: true\n")
		require.NoError(t, err)
	})
	t.Run("unsupported apiVersion", func(t *testing.T) {
		_, err := convertDescriptorContent(t, "apiVersion: openchoreo.dev/v2\nmetadata:\n  name: greeter\n")
		require.Error(t, err)
		assert.Contains(t, err.Error(), `unsupported descriptor apiVersion "openchoreo.dev/v2"`)
	})
}
