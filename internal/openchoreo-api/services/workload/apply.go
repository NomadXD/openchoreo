// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package workload

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"

	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	openchoreov1alpha1 "github.com/openchoreo/openchoreo/api/v1alpha1"
	"github.com/openchoreo/openchoreo/internal/labels"
	"github.com/openchoreo/openchoreo/internal/openchoreo-api/services"
	"github.com/openchoreo/openchoreo/internal/releaseline"
	"github.com/openchoreo/openchoreo/internal/workloadexposure"
)

// AnnotationEndpointsManagedBy marks a Workload whose Endpoints are managed by the workload
// descriptor it was applied from, so clients can tell a component's exposure lives in its
// repository.
const AnnotationEndpointsManagedBy = "openchoreo.dev/endpoints-managed-by"

// ApplyRequest is a Workload together with the Endpoints its descriptor asks for.
type ApplyRequest struct {
	Workload *openchoreov1alpha1.Workload
	// ManageEndpoints is true when the descriptor can express exposure (v1alpha2). Exposures are
	// then the complete set: descriptor-managed Endpoints not among them are deleted. When false,
	// Endpoints are left alone.
	ManageEndpoints bool
	Exposures       []workloadexposure.Exposure
}

// ApplyPlan is what applying a request changes. Planning resolves and validates everything
// before anything is written, so a bad exposure fails the build with nothing half-applied.
type ApplyPlan struct {
	Workload        *openchoreov1alpha1.Workload
	WorkloadExists  bool
	ManageEndpoints bool
	// Create are new Endpoints.
	Create []openchoreov1alpha1.Endpoint
	// Update are existing Endpoints with their new spec and labels.
	Update []openchoreov1alpha1.Endpoint
	// Replace are Endpoints whose EndpointType changed. The type is immutable, so each is
	// deleted and created again under the same name.
	Replace []openchoreov1alpha1.Endpoint
	// Delete are descriptor-managed Endpoints the descriptor no longer asks for.
	Delete []openchoreov1alpha1.Endpoint
	// Unchanged names Endpoints that already match.
	Unchanged []string
}

// ApplyResult reports what applying a plan changed.
type ApplyResult struct {
	Workload  *openchoreov1alpha1.Workload
	Created   []string
	Updated   []string
	Replaced  []string
	Deleted   []string
	Unchanged []string
}

func (s *workloadService) PlanApply(ctx context.Context, namespaceName string, req *ApplyRequest) (*ApplyPlan, error) {
	if req == nil || req.Workload == nil {
		return nil, fmt.Errorf("workload cannot be nil")
	}
	w := req.Workload
	owner := w.Spec.Owner
	line := releaseline.Normalize(owner.ReleaseLine)

	component := &openchoreov1alpha1.Component{}
	if err := s.k8sClient.Get(ctx, client.ObjectKey{Namespace: namespaceName, Name: owner.ComponentName}, component); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, ErrComponentNotFound
		}
		return nil, fmt.Errorf("failed to get component: %w", err)
	}
	if component.Spec.Owner.ProjectName != owner.ProjectName {
		return nil, &services.ValidationError{Msg: fmt.Sprintf("component %q belongs to project %q, not %q",
			owner.ComponentName, component.Spec.Owner.ProjectName, owner.ProjectName)}
	}
	if err := services.ValidateReleaseLineOwner(ctx, s.k8sClient, namespaceName,
		owner.ProjectName, owner.ComponentName, owner.ReleaseLine); err != nil {
		return nil, err
	}

	exists, err := s.workloadExists(ctx, namespaceName, w.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to check workload existence: %w", err)
	}
	plan := &ApplyPlan{Workload: w, WorkloadExists: exists, ManageEndpoints: req.ManageEndpoints}
	if !req.ManageEndpoints {
		return plan, nil
	}

	autoDeploy, err := s.lineAutoDeploy(ctx, namespaceName, component, line)
	if err != nil {
		return nil, err
	}
	desired, err := workloadexposure.Generate(ctx, workloadexposure.Target{
		Namespace:     namespaceName,
		Project:       owner.ProjectName,
		Component:     owner.ComponentName,
		ReleaseLine:   line,
		ComponentType: component.Spec.ComponentType,
		AutoDeploy:    autoDeploy,
		Endpoints:     w.Spec.Endpoints,
	}, req.Exposures, workloadexposure.ClientLookup{Client: s.k8sClient})
	if err != nil {
		if invalid, ok := errors.AsType[*workloadexposure.InvalidExposureError](err); ok {
			return nil, &services.ValidationError{Msg: invalid.Msg, StatusCode: http.StatusUnprocessableEntity}
		}
		return nil, err
	}

	managed, err := s.descriptorManagedEndpoints(ctx, namespaceName, owner.ComponentName, line)
	if err != nil {
		return nil, err
	}
	wanted := make(map[string]bool, len(desired))
	for i := range desired {
		want := &desired[i]
		wanted[want.Name] = true
		current, ok := managed[want.Name]
		if !ok {
			current, err = s.adoptableEndpoint(ctx, namespaceName, want, line)
			if err != nil {
				return nil, err
			}
		}
		switch {
		case current == nil:
			plan.Create = append(plan.Create, *want)
		case current.Spec.Type != want.Spec.Type:
			plan.Replace = append(plan.Replace, *want)
		default:
			updated := mergeEndpoint(current, want)
			if equality.Semantic.DeepEqual(current.Spec, updated.Spec) && equality.Semantic.DeepEqual(current.Labels, updated.Labels) {
				plan.Unchanged = append(plan.Unchanged, want.Name)
			} else {
				plan.Update = append(plan.Update, *updated)
			}
		}
	}
	for _, name := range sortedKeys(managed) {
		if !wanted[name] {
			plan.Delete = append(plan.Delete, *managed[name])
		}
	}
	return plan, nil
}

// lineAutoDeploy is the line's effective autoDeploy: the ReleaseLine's override when it sets
// one, otherwise the component's.
func (s *workloadService) lineAutoDeploy(ctx context.Context, namespaceName string, component *openchoreov1alpha1.Component, line string) (bool, error) {
	if line == "" {
		return component.Spec.AutoDeploy, nil
	}
	rl := &openchoreov1alpha1.ReleaseLine{}
	if err := s.k8sClient.Get(ctx, client.ObjectKey{Namespace: namespaceName, Name: line}, rl); err != nil {
		return false, fmt.Errorf("failed to get release line %q: %w", line, err)
	}
	if rl.Spec.AutoDeploy != nil {
		return *rl.Spec.AutoDeploy, nil
	}
	return component.Spec.AutoDeploy, nil
}

// descriptorManagedEndpoints lists the Endpoints a descriptor manages for a component's line.
func (s *workloadService) descriptorManagedEndpoints(ctx context.Context, namespaceName, componentName, line string) (map[string]*openchoreov1alpha1.Endpoint, error) {
	var list openchoreov1alpha1.EndpointList
	if err := s.k8sClient.List(ctx, &list, client.InNamespace(namespaceName), client.MatchingLabels{
		labels.LabelKeySourceComponent: componentName,
		labels.LabelKeyManagedBy:       workloadexposure.ManagedByWorkloadDescriptor,
	}); err != nil {
		return nil, fmt.Errorf("failed to list endpoints: %w", err)
	}
	managed := make(map[string]*openchoreov1alpha1.Endpoint, len(list.Items))
	for i := range list.Items {
		ep := &list.Items[i]
		if ep.Labels[labels.LabelKeySourceReleaseLine] == line {
			managed[ep.Name] = ep
		}
	}
	return managed, nil
}

// adoptableEndpoint returns an existing Endpoint that already takes the desired one's name. A
// component's own Endpoint for the same workload endpoint and line, created in the portal, is
// adopted: the descriptor manages it from now on. Anything else holding the name is a conflict.
func (s *workloadService) adoptableEndpoint(ctx context.Context, namespaceName string, want *openchoreov1alpha1.Endpoint, line string) (*openchoreov1alpha1.Endpoint, error) {
	existing := &openchoreov1alpha1.Endpoint{}
	if err := s.k8sClient.Get(ctx, client.ObjectKey{Namespace: namespaceName, Name: want.Name}, existing); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get endpoint %q: %w", want.Name, err)
	}
	sameSource := existing.Labels[labels.LabelKeySourceComponent] == want.Labels[labels.LabelKeySourceComponent] &&
		existing.Labels[labels.LabelKeySourceEndpoint] == want.Labels[labels.LabelKeySourceEndpoint] &&
		existing.Labels[labels.LabelKeySourceReleaseLine] == line
	if !sameSource {
		return nil, &services.ValidationError{
			Msg: fmt.Sprintf("Endpoint %q already exists and is not one of this component's Endpoints; "+
				"rename the expose entry or remove that Endpoint", want.Name),
			StatusCode: http.StatusConflict,
		}
	}
	return existing, nil
}

// mergeEndpoint applies the desired spec and labels to a copy of an existing Endpoint, keeping
// what the descriptor does not say: the weight a backend has, and labels set by others.
func mergeEndpoint(current, want *openchoreov1alpha1.Endpoint) *openchoreov1alpha1.Endpoint {
	updated := current.DeepCopy()
	weights := make(map[string]*int32, len(current.Spec.BackendRefs))
	for _, ref := range current.Spec.BackendRefs {
		weights[ref.Name] = ref.Weight
	}
	backendRefs := make([]openchoreov1alpha1.EndpointBackendRef, len(want.Spec.BackendRefs))
	for i, ref := range want.Spec.BackendRefs {
		ref = *ref.DeepCopy()
		if ref.Weight == nil {
			ref.Weight = weights[ref.Name]
		}
		backendRefs[i] = ref
	}
	updated.Spec.Visibility = want.Spec.Visibility
	updated.Spec.BackendRefs = backendRefs
	updated.Spec.Parameters = want.Spec.Parameters
	updated.Spec.AutoDeploy = want.Spec.AutoDeploy
	if updated.Labels == nil {
		updated.Labels = map[string]string{}
	}
	for k, v := range want.Labels {
		updated.Labels[k] = v
	}
	return updated
}

func (s *workloadService) ApplyWorkload(ctx context.Context, namespaceName string, plan *ApplyPlan) (*ApplyResult, error) {
	if plan == nil || plan.Workload == nil {
		return nil, fmt.Errorf("plan cannot be nil")
	}
	w := plan.Workload
	if plan.ManageEndpoints {
		if w.Annotations == nil {
			w.Annotations = map[string]string{}
		}
		w.Annotations[AnnotationEndpointsManagedBy] = workloadexposure.ManagedByWorkloadDescriptor
	}

	// The Workload goes first: an Endpoint is checked against the workload endpoint it routes to.
	var applied *openchoreov1alpha1.Workload
	var err error
	if plan.WorkloadExists {
		applied, err = s.UpdateWorkload(ctx, namespaceName, w)
	} else {
		applied, err = s.CreateWorkload(ctx, namespaceName, w)
	}
	if err != nil {
		return nil, err
	}

	result := &ApplyResult{Workload: applied, Unchanged: plan.Unchanged}
	for i := range plan.Delete {
		if err := s.deleteEndpoint(ctx, &plan.Delete[i]); err != nil {
			return nil, err
		}
		result.Deleted = append(result.Deleted, plan.Delete[i].Name)
	}
	for i := range plan.Replace {
		ep := plan.Replace[i]
		if err := s.deleteEndpoint(ctx, &ep); err != nil {
			return nil, err
		}
		if err := s.createEndpoint(ctx, &ep); err != nil {
			return nil, err
		}
		result.Replaced = append(result.Replaced, ep.Name)
	}
	for i := range plan.Create {
		ep := plan.Create[i]
		if err := s.createEndpoint(ctx, &ep); err != nil {
			return nil, err
		}
		result.Created = append(result.Created, ep.Name)
	}
	for i := range plan.Update {
		ep := plan.Update[i]
		if err := s.k8sClient.Update(ctx, &ep); err != nil {
			return nil, endpointWriteError(err, "update", ep.Name)
		}
		result.Updated = append(result.Updated, ep.Name)
	}
	s.logger.Info("Workload applied", "namespace", namespaceName, "workload", w.Name,
		"created", result.Created, "updated", result.Updated, "replaced", result.Replaced, "deleted", result.Deleted)
	return result, nil
}

func (s *workloadService) createEndpoint(ctx context.Context, ep *openchoreov1alpha1.Endpoint) error {
	ep.ResourceVersion = ""
	ep.UID = ""
	if err := s.k8sClient.Create(ctx, ep); err != nil {
		return endpointWriteError(err, "create", ep.Name)
	}
	return nil
}

// deleteEndpoint deletes an Endpoint and waits for nothing: a replacement created under the same
// name while the old one is still finalizing is rejected, so the build is retried rather than
// racing the finalizer.
func (s *workloadService) deleteEndpoint(ctx context.Context, ep *openchoreov1alpha1.Endpoint) error {
	if err := s.k8sClient.Delete(ctx, ep); err != nil && !apierrors.IsNotFound(err) {
		return endpointWriteError(err, "delete", ep.Name)
	}
	return nil
}

func endpointWriteError(err error, verb, name string) error {
	if vErr := services.ExtractValidationError(err); vErr != nil {
		vErr.Msg = fmt.Sprintf("Endpoint %q: %s", name, vErr.Msg)
		return vErr
	}
	if apierrors.IsAlreadyExists(err) {
		return &services.ValidationError{
			Msg:        fmt.Sprintf("Endpoint %q is still being deleted; retry the build once it is gone", name),
			StatusCode: http.StatusConflict,
		}
	}
	return fmt.Errorf("failed to %s endpoint %q: %w", verb, name, err)
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
