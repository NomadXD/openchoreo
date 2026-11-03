// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package workload

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	openchoreov1alpha1 "github.com/openchoreo/openchoreo/api/v1alpha1"
	"github.com/openchoreo/openchoreo/internal/labels"
	"github.com/openchoreo/openchoreo/internal/openchoreo-api/services"
	"github.com/openchoreo/openchoreo/internal/openchoreo-api/services/testutil"
	"github.com/openchoreo/openchoreo/internal/workloadexposure"
)

const applyEndpointName = testComponentName + "-http"

func applyFixtures() []client.Object {
	ct := &openchoreov1alpha1.ComponentType{
		ObjectMeta: metav1.ObjectMeta{Name: "web-app", Namespace: testNamespace},
		Spec: openchoreov1alpha1.ComponentTypeSpec{
			AllowedEndpoints: []openchoreov1alpha1.AllowedEndpointType{
				{Name: "web", Protocols: []openchoreov1alpha1.EndpointProtocol{openchoreov1alpha1.EndpointProtocolHTTP},
					Ref: openchoreov1alpha1.EndpointTypeRef{Kind: openchoreov1alpha1.EndpointTypeRefKindEndpointType, Name: "http-route"}, Default: true},
				{Name: "managed", Protocols: []openchoreov1alpha1.EndpointProtocol{openchoreov1alpha1.EndpointProtocolHTTP},
					Ref: openchoreov1alpha1.EndpointTypeRef{Kind: openchoreov1alpha1.EndpointTypeRefKindEndpointType, Name: "managed-http"}},
			},
		},
	}
	types := make([]client.Object, 0, 4)
	types = append(types, ct)
	for _, name := range []string{"http-route", "managed-http"} {
		types = append(types, &openchoreov1alpha1.EndpointType{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace}})
	}
	comp := testutil.NewComponent(testNamespace, testProjectName, testComponentName)
	comp.Spec.AutoDeploy = true
	return append(types, comp)
}

func applyWorkloadCR() *openchoreov1alpha1.Workload {
	w := testutil.NewWorkload(testNamespace, testProjectName, testComponentName, testWorkloadName)
	w.Spec.Endpoints = map[string]openchoreov1alpha1.WorkloadEndpoint{
		"http": {Port: 8080, Protocol: openchoreov1alpha1.EndpointProtocolHTTP},
	}
	return w
}

func exposeHTTP(via string, params string) []workloadexposure.Exposure {
	exposure := workloadexposure.Exposure{
		Endpoint:   "http",
		Visibility: []openchoreov1alpha1.EndpointVisibility{openchoreov1alpha1.EndpointVisibilityExternal},
		Via:        via,
	}
	if params != "" {
		exposure.Parameters = &runtime.RawExtension{Raw: []byte(params)}
	}
	return []workloadexposure.Exposure{exposure}
}

func apply(t *testing.T, svc Service, req *ApplyRequest) (*ApplyResult, error) {
	t.Helper()
	plan, err := svc.PlanApply(context.Background(), testNamespace, req)
	if err != nil {
		return nil, err
	}
	return svc.ApplyWorkload(context.Background(), testNamespace, plan)
}

func getEndpoint(t *testing.T, c client.Client, name string) *openchoreov1alpha1.Endpoint {
	t.Helper()
	ep := &openchoreov1alpha1.Endpoint{}
	require.NoError(t, c.Get(context.Background(), client.ObjectKey{Namespace: testNamespace, Name: name}, ep))
	return ep
}

func TestApplyWorkload(t *testing.T) {
	ctx := context.Background()

	t.Run("creates the workload and its Endpoints, then converges", func(t *testing.T) {
		c := testutil.NewFakeClient(applyFixtures()...)
		svc := NewService(c, testutil.TestLogger())

		result, err := apply(t, svc, &ApplyRequest{Workload: applyWorkloadCR(), ManageEndpoints: true, Exposures: exposeHTTP("", "")})
		require.NoError(t, err)
		assert.Equal(t, []string{applyEndpointName}, result.Created)
		assert.Equal(t, workloadexposure.ManagedByWorkloadDescriptor, result.Workload.Annotations[AnnotationEndpointsManagedBy])

		ep := getEndpoint(t, c, applyEndpointName)
		assert.Equal(t, "http-route", ep.Spec.Type.Name)
		assert.True(t, ep.Spec.AutoDeploy)
		assert.Equal(t, workloadexposure.ManagedByWorkloadDescriptor, ep.Labels[labels.LabelKeyManagedBy])

		result, err = apply(t, svc, &ApplyRequest{Workload: applyWorkloadCR(), ManageEndpoints: true, Exposures: exposeHTTP("", "")})
		require.NoError(t, err)
		assert.Empty(t, result.Created)
		assert.Empty(t, result.Updated)
		assert.Equal(t, []string{applyEndpointName}, result.Unchanged)
	})

	t.Run("updates parameters and keeps a backend's weight", func(t *testing.T) {
		c := testutil.NewFakeClient(applyFixtures()...)
		svc := NewService(c, testutil.TestLogger())
		_, err := apply(t, svc, &ApplyRequest{Workload: applyWorkloadCR(), ManageEndpoints: true, Exposures: exposeHTTP("managed", "")})
		require.NoError(t, err)

		ep := getEndpoint(t, c, applyEndpointName)
		ep.Spec.BackendRefs[0].Weight = ptr.To[int32](5)
		require.NoError(t, c.Update(ctx, ep))

		result, err := apply(t, svc, &ApplyRequest{Workload: applyWorkloadCR(), ManageEndpoints: true, Exposures: exposeHTTP("managed", `{"requireApiKey":true}`)})
		require.NoError(t, err)
		assert.Equal(t, []string{applyEndpointName}, result.Updated)

		ep = getEndpoint(t, c, applyEndpointName)
		assert.JSONEq(t, `{"requireApiKey":true}`, string(ep.Spec.Parameters.Raw))
		assert.Equal(t, ptr.To[int32](5), ep.Spec.BackendRefs[0].Weight)
	})

	t.Run("replaces an Endpoint whose type changed", func(t *testing.T) {
		c := testutil.NewFakeClient(applyFixtures()...)
		svc := NewService(c, testutil.TestLogger())
		_, err := apply(t, svc, &ApplyRequest{Workload: applyWorkloadCR(), ManageEndpoints: true, Exposures: exposeHTTP("", "")})
		require.NoError(t, err)

		result, err := apply(t, svc, &ApplyRequest{Workload: applyWorkloadCR(), ManageEndpoints: true, Exposures: exposeHTTP("managed", "")})
		require.NoError(t, err)
		assert.Equal(t, []string{applyEndpointName}, result.Replaced)
		assert.Equal(t, "managed-http", getEndpoint(t, c, applyEndpointName).Spec.Type.Name)
	})

	t.Run("an empty list deletes descriptor-managed Endpoints only", func(t *testing.T) {
		portal := &openchoreov1alpha1.Endpoint{
			ObjectMeta: metav1.ObjectMeta{Name: "portal-made", Namespace: testNamespace, Labels: map[string]string{
				labels.LabelKeySourceComponent: testComponentName,
				labels.LabelKeySourceEndpoint:  "http",
			}},
		}
		c := testutil.NewFakeClient(append(applyFixtures(), portal)...)
		svc := NewService(c, testutil.TestLogger())
		_, err := apply(t, svc, &ApplyRequest{Workload: applyWorkloadCR(), ManageEndpoints: true, Exposures: exposeHTTP("", "")})
		require.NoError(t, err)

		result, err := apply(t, svc, &ApplyRequest{Workload: applyWorkloadCR(), ManageEndpoints: true, Exposures: []workloadexposure.Exposure{}})
		require.NoError(t, err)
		assert.Equal(t, []string{applyEndpointName}, result.Deleted)

		err = c.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: applyEndpointName}, &openchoreov1alpha1.Endpoint{})
		assert.True(t, apierrors.IsNotFound(err))
		getEndpoint(t, c, "portal-made")
	})

	t.Run("without exposures Endpoints are left alone", func(t *testing.T) {
		c := testutil.NewFakeClient(applyFixtures()...)
		svc := NewService(c, testutil.TestLogger())
		_, err := apply(t, svc, &ApplyRequest{Workload: applyWorkloadCR(), ManageEndpoints: true, Exposures: exposeHTTP("", "")})
		require.NoError(t, err)

		result, err := apply(t, svc, &ApplyRequest{Workload: applyWorkloadCR()})
		require.NoError(t, err)
		assert.Empty(t, result.Deleted)
		assert.Empty(t, result.Workload.Annotations[AnnotationEndpointsManagedBy])
		getEndpoint(t, c, applyEndpointName)
	})

	t.Run("adopts the component's own Endpoint of the same name", func(t *testing.T) {
		portal := &openchoreov1alpha1.Endpoint{
			ObjectMeta: metav1.ObjectMeta{Name: applyEndpointName, Namespace: testNamespace, Labels: map[string]string{
				labels.LabelKeySourceComponent: testComponentName,
				labels.LabelKeySourceEndpoint:  "http",
			}},
			Spec: openchoreov1alpha1.EndpointSpec{
				Type: openchoreov1alpha1.EndpointTypeRef{Kind: openchoreov1alpha1.EndpointTypeRefKindEndpointType, Name: "http-route"},
			},
		}
		c := testutil.NewFakeClient(append(applyFixtures(), portal)...)
		svc := NewService(c, testutil.TestLogger())

		result, err := apply(t, svc, &ApplyRequest{Workload: applyWorkloadCR(), ManageEndpoints: true, Exposures: exposeHTTP("", "")})
		require.NoError(t, err)
		assert.Equal(t, []string{applyEndpointName}, result.Updated)
		assert.Equal(t, workloadexposure.ManagedByWorkloadDescriptor, getEndpoint(t, c, applyEndpointName).Labels[labels.LabelKeyManagedBy])
	})

	t.Run("refuses a name held by another Endpoint", func(t *testing.T) {
		other := &openchoreov1alpha1.Endpoint{
			ObjectMeta: metav1.ObjectMeta{Name: applyEndpointName, Namespace: testNamespace},
		}
		c := testutil.NewFakeClient(append(applyFixtures(), other)...)
		svc := NewService(c, testutil.TestLogger())

		_, err := apply(t, svc, &ApplyRequest{Workload: applyWorkloadCR(), ManageEndpoints: true, Exposures: exposeHTTP("", "")})
		var vErr *services.ValidationError
		require.True(t, errors.As(err, &vErr))
		assert.Equal(t, http.StatusConflict, vErr.StatusCode)
	})

	t.Run("an invalid exposure writes nothing", func(t *testing.T) {
		c := testutil.NewFakeClient(applyFixtures()...)
		svc := NewService(c, testutil.TestLogger())

		_, err := apply(t, svc, &ApplyRequest{Workload: applyWorkloadCR(), ManageEndpoints: true, Exposures: exposeHTTP("nope", "")})
		var vErr *services.ValidationError
		require.True(t, errors.As(err, &vErr))
		assert.Equal(t, http.StatusUnprocessableEntity, vErr.StatusCode)
		assert.Contains(t, vErr.Msg, `via "nope"`)

		err = c.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: testWorkloadName}, &openchoreov1alpha1.Workload{})
		assert.True(t, apierrors.IsNotFound(err), "the workload must not be written")
	})

	t.Run("component not found", func(t *testing.T) {
		svc := NewService(testutil.NewFakeClient(), testutil.TestLogger())
		_, err := apply(t, svc, &ApplyRequest{Workload: applyWorkloadCR()})
		require.ErrorIs(t, err, ErrComponentNotFound)
	})
}
