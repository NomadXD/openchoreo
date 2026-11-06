// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package endpoint

import (
	"encoding/json"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"

	openchoreov1alpha1 "github.com/openchoreo/openchoreo/api/v1alpha1"
	"github.com/openchoreo/openchoreo/pkg/hash"
)

// ReleaseSpec captures the immutable inputs that uniquely identify an
// EndpointRelease. Owner is intentionally excluded so the hash represents the
// configuration, not the ownership pointer (mirrors resource.ReleaseSpec).
type ReleaseSpec struct {
	EndpointType openchoreov1alpha1.EndpointReleaseEndpointType `json:"endpointType"`
	Visibility   []openchoreov1alpha1.EndpointVisibility        `json:"visibility"`
	BackendRefs  []openchoreov1alpha1.EndpointBackendRef        `json:"backendRefs"`
	Schema       *openchoreov1alpha1.Schema                     `json:"schema,omitempty"`
	Parameters   *runtime.RawExtension                          `json:"parameters,omitempty"`
}

// computeReleaseHash returns a deterministic hash of a ReleaseSpec's canonical
// JSON: object keys sorted and unset optional fields left out, so a field
// added later doesn't change the hash of an Endpoint that doesn't set it.
func computeReleaseHash(spec ReleaseSpec) (string, error) {
	raw, err := json.Marshal(spec)
	if err != nil {
		return "", fmt.Errorf("marshal release spec: %w", err)
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", fmt.Errorf("unmarshal release spec: %w", err)
	}
	canonical, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("marshal canonical release spec: %w", err)
	}
	return hash.ComputeHashOfString(string(canonical), nil), nil
}
