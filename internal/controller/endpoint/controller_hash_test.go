// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package endpoint

import (
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"

	openchoreov1alpha1 "github.com/openchoreo/openchoreo/api/v1alpha1"
)

func specWith(backend openchoreov1alpha1.EndpointBackendRef) ReleaseSpec {
	return ReleaseSpec{
		Visibility:  []openchoreov1alpha1.EndpointVisibility{openchoreov1alpha1.EndpointVisibilityExternal},
		BackendRefs: []openchoreov1alpha1.EndpointBackendRef{backend},
	}
}

func stableBackend() openchoreov1alpha1.EndpointBackendRef {
	return openchoreov1alpha1.EndpointBackendRef{
		Name:      "stable",
		Kind:      openchoreov1alpha1.EndpointBackendKindComponent,
		Component: &openchoreov1alpha1.EndpointComponentBackend{Name: "greeter", Endpoint: "http"},
		Weight:    ptr.To(int32(90)),
	}
}

func mustHash(t *testing.T, spec ReleaseSpec) string {
	t.Helper()
	h, err := computeReleaseHash(spec)
	if err != nil {
		t.Fatalf("computeReleaseHash: %v", err)
	}
	return h
}

func TestReleaseHash_UnsetFieldsLeaveItUnchanged(t *testing.T) {
	plain := stableBackend()
	empty := stableBackend()
	empty.Component.ReleaseLine = ""
	empty.Match, empty.Mirror = nil, nil
	if mustHash(t, specWith(plain)) != mustHash(t, specWith(empty)) {
		t.Fatalf("expected unset release line, match and mirror to leave the hash unchanged")
	}
}

func TestReleaseHash_SetFieldsChangeIt(t *testing.T) {
	base := mustHash(t, specWith(stableBackend()))

	line := stableBackend()
	line.Component.ReleaseLine = "beta"
	matched := stableBackend()
	matched.Match = &openchoreov1alpha1.EndpointMatch{
		Headers: []openchoreov1alpha1.EndpointHeaderMatch{{Name: "x-canary", Value: "true"}},
	}
	mirrored := stableBackend()
	mirrored.Weight = ptr.To(int32(0))
	mirrored.Mirror = &openchoreov1alpha1.EndpointMirror{Percent: 10}

	for name, b := range map[string]openchoreov1alpha1.EndpointBackendRef{
		"release line": line, "match": matched, "mirror": mirrored,
	} {
		if mustHash(t, specWith(b)) == base {
			t.Errorf("expected setting a %s to change the release hash", name)
		}
	}
}

func TestReleaseHash_IgnoresParameterKeyOrder(t *testing.T) {
	a := specWith(stableBackend())
	a.Parameters = &runtime.RawExtension{Raw: []byte(`{"pathPrefix":"/trips","timeout":"5s"}`)}
	b := specWith(stableBackend())
	b.Parameters = &runtime.RawExtension{Raw: []byte(`{"timeout":"5s","pathPrefix":"/trips"}`)}
	if mustHash(t, a) != mustHash(t, b) {
		t.Fatalf("expected the same parameters in another key order to hash the same")
	}
}

func TestReleaseHash_HeaderValueCannotPassForAField(t *testing.T) {
	plain := stableBackend()
	tricky := stableBackend()
	tricky.Match = &openchoreov1alpha1.EndpointMatch{
		Headers: []openchoreov1alpha1.EndpointHeaderMatch{{Name: "x", Value: ` Mirror:(*v1alpha1.EndpointMirror)<nil>}`}},
	}
	if mustHash(t, specWith(plain)) == mustHash(t, specWith(tricky)) {
		t.Fatalf("expected a header value to be hashed as data")
	}
}
