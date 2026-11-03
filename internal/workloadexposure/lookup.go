// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package workloadexposure

import (
	"context"
	"fmt"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openchoreo/openchoreo/api/v1alpha1"
)

// ClientLookup reads ComponentTypes and EndpointTypes from the cluster.
type ClientLookup struct {
	Client client.Reader
}

var _ Lookup = ClientLookup{}

// AllowedEndpoints returns the allowedEndpoints of the referenced (Cluster)ComponentType. The
// reference's name is `{workloadType}/{name}`.
func (l ClientLookup) AllowedEndpoints(ctx context.Context, namespace string, ref v1alpha1.ComponentTypeRef) ([]v1alpha1.AllowedEndpointType, error) {
	_, name, ok := strings.Cut(ref.Name, "/")
	if !ok {
		return nil, fmt.Errorf("invalid component type reference %q: expected {workloadType}/{name}", ref.Name)
	}
	if ref.Kind == v1alpha1.ComponentTypeRefKindClusterComponentType {
		var cct v1alpha1.ClusterComponentType
		if err := l.Client.Get(ctx, client.ObjectKey{Name: name}, &cct); err != nil {
			return nil, notFoundAsInvalid(err, "ClusterComponentType", name)
		}
		return cct.Spec.AllowedEndpoints, nil
	}
	var ct v1alpha1.ComponentType
	if err := l.Client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &ct); err != nil {
		return nil, notFoundAsInvalid(err, "ComponentType", name)
	}
	return ct.Spec.AllowedEndpoints, nil
}

// EndpointTypeParameters returns the parameters schema of the referenced (Cluster)EndpointType.
func (l ClientLookup) EndpointTypeParameters(ctx context.Context, namespace string, ref v1alpha1.EndpointTypeRef) (*v1alpha1.SchemaSection, error) {
	if ref.Kind == v1alpha1.EndpointTypeRefKindClusterEndpointType {
		var cet v1alpha1.ClusterEndpointType
		if err := l.Client.Get(ctx, client.ObjectKey{Name: ref.Name}, &cet); err != nil {
			return nil, notFoundAsInvalid(err, "ClusterEndpointType", ref.Name)
		}
		return cet.Spec.Parameters, nil
	}
	var et v1alpha1.EndpointType
	if err := l.Client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: ref.Name}, &et); err != nil {
		return nil, notFoundAsInvalid(err, "EndpointType", ref.Name)
	}
	return et.Spec.Parameters, nil
}

// notFoundAsInvalid reports a missing type as an invalid exposure, so the build that applies
// the descriptor says which type is missing instead of failing with a server error.
func notFoundAsInvalid(err error, kind, name string) error {
	if apierrors.IsNotFound(err) {
		return invalid("%s %s does not exist", kind, name)
	}
	return fmt.Errorf("failed to get %s %s: %w", kind, name, err)
}
