// Copyright 2025 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"

	openchoreov1alpha1 "github.com/openchoreo/openchoreo/api/v1alpha1"
	"github.com/openchoreo/openchoreo/internal/openchoreo-api/api/gen"
	"github.com/openchoreo/openchoreo/internal/openchoreo-api/services"
	workloadsvc "github.com/openchoreo/openchoreo/internal/openchoreo-api/services/workload"
	"github.com/openchoreo/openchoreo/internal/server/middleware/audit"
	"github.com/openchoreo/openchoreo/internal/workloadexposure"
)

// ListWorkloads returns a paginated list of workloads within a namespace.
func (h *Handler) ListWorkloads(
	ctx context.Context,
	request gen.ListWorkloadsRequestObject,
) (gen.ListWorkloadsResponseObject, error) {
	h.logger.Debug("ListWorkloads called", "namespaceName", request.NamespaceName)

	componentName := ""
	if request.Params.Component != nil {
		componentName = *request.Params.Component
	}

	opts := NormalizeListOptions(request.Params.Limit, request.Params.Cursor, request.Params.LabelSelector)

	result, err := h.services.WorkloadService.ListWorkloads(ctx, request.NamespaceName, componentName, opts)
	if err != nil {
		if validationErr, ok := errors.AsType[*services.ValidationError](err); ok {
			return gen.ListWorkloads400JSONResponse{BadRequestJSONResponse: badRequest(validationErr.Msg)}, nil
		}
		h.logger.Error("Failed to list workloads", "error", err)
		return gen.ListWorkloads500JSONResponse{InternalErrorJSONResponse: internalError()}, nil
	}

	items, err := convertList[openchoreov1alpha1.Workload, gen.Workload](result.Items)
	if err != nil {
		h.logger.Error("Failed to convert workloads", "error", err)
		return gen.ListWorkloads500JSONResponse{InternalErrorJSONResponse: internalError()}, nil
	}

	return gen.ListWorkloads200JSONResponse{
		Items:      items,
		Pagination: ToPagination(result),
	}, nil
}

// CreateWorkload creates a new workload within a namespace.
func (h *Handler) CreateWorkload(
	ctx context.Context,
	request gen.CreateWorkloadRequestObject,
) (gen.CreateWorkloadResponseObject, error) {
	h.logger.Info("CreateWorkload called", "namespaceName", request.NamespaceName)

	if request.Body == nil {
		return gen.CreateWorkload400JSONResponse{BadRequestJSONResponse: badRequest("Request body is required")}, nil
	}

	wCR, err := convert[gen.Workload, openchoreov1alpha1.Workload](*request.Body)
	if err != nil {
		h.logger.Error("Failed to convert create request", "error", err)
		return gen.CreateWorkload400JSONResponse{BadRequestJSONResponse: badRequest("Invalid request body")}, nil
	}

	created, err := h.services.WorkloadService.CreateWorkload(ctx, request.NamespaceName, &wCR)
	if err != nil {
		if errors.Is(err, services.ErrForbidden) {
			return gen.CreateWorkload403JSONResponse{ForbiddenJSONResponse: forbidden()}, nil
		}
		if errors.Is(err, workloadsvc.ErrWorkloadAlreadyExists) {
			return gen.CreateWorkload409JSONResponse{ConflictJSONResponse: conflict("Workload already exists")}, nil
		}
		if errors.Is(err, workloadsvc.ErrComponentNotFound) {
			return gen.CreateWorkload400JSONResponse{BadRequestJSONResponse: badRequest("Referenced component not found")}, nil
		}
		if validationErr, ok := errors.AsType[*services.ValidationError](err); ok {
			if validationErr.StatusCode == http.StatusUnprocessableEntity {
				return gen.CreateWorkload422JSONResponse{UnprocessableContentJSONResponse: unprocessableContent(validationErr.Msg)}, nil
			}
			return gen.CreateWorkload400JSONResponse{BadRequestJSONResponse: badRequest(validationErr.Msg)}, nil
		}
		h.logger.Error("Failed to create workload", "error", err)
		return gen.CreateWorkload500JSONResponse{InternalErrorJSONResponse: internalError()}, nil
	}

	audit.SetResource(ctx, &audit.Resource{Namespace: request.NamespaceName, UID: string(created.UID), Name: created.Name})

	genWorkload, err := convert[openchoreov1alpha1.Workload, gen.Workload](*created)
	if err != nil {
		h.logger.Error("Failed to convert created workload", "error", err)
		return gen.CreateWorkload500JSONResponse{InternalErrorJSONResponse: internalError()}, nil
	}

	h.logger.Info("Workload created successfully", "namespaceName", request.NamespaceName, "workload", created.Name)
	return gen.CreateWorkload201JSONResponse(genWorkload), nil
}

// GetWorkload returns details of a specific workload.
func (h *Handler) GetWorkload(
	ctx context.Context,
	request gen.GetWorkloadRequestObject,
) (gen.GetWorkloadResponseObject, error) {
	h.logger.Debug("GetWorkload called", "namespaceName", request.NamespaceName, "workloadName", request.WorkloadName)

	w, err := h.services.WorkloadService.GetWorkload(ctx, request.NamespaceName, request.WorkloadName)
	if err != nil {
		if errors.Is(err, services.ErrForbidden) {
			return gen.GetWorkload403JSONResponse{ForbiddenJSONResponse: forbidden()}, nil
		}
		if errors.Is(err, workloadsvc.ErrWorkloadNotFound) {
			return gen.GetWorkload404JSONResponse{NotFoundJSONResponse: notFound("Workload")}, nil
		}
		h.logger.Error("Failed to get workload", "error", err)
		return gen.GetWorkload500JSONResponse{InternalErrorJSONResponse: internalError()}, nil
	}

	genWorkload, err := convert[openchoreov1alpha1.Workload, gen.Workload](*w)
	if err != nil {
		h.logger.Error("Failed to convert workload", "error", err)
		return gen.GetWorkload500JSONResponse{InternalErrorJSONResponse: internalError()}, nil
	}

	return gen.GetWorkload200JSONResponse(genWorkload), nil
}

// UpdateWorkload replaces an existing workload (full update).
func (h *Handler) UpdateWorkload(
	ctx context.Context,
	request gen.UpdateWorkloadRequestObject,
) (gen.UpdateWorkloadResponseObject, error) {
	h.logger.Info("UpdateWorkload called", "namespaceName", request.NamespaceName, "workloadName", request.WorkloadName)

	if request.Body == nil {
		return gen.UpdateWorkload400JSONResponse{BadRequestJSONResponse: badRequest("Request body is required")}, nil
	}

	wCR, err := convert[gen.Workload, openchoreov1alpha1.Workload](*request.Body)
	if err != nil {
		h.logger.Error("Failed to convert update request", "error", err)
		return gen.UpdateWorkload400JSONResponse{BadRequestJSONResponse: badRequest("Invalid request body")}, nil
	}

	// Ensure the name from the URL path is used
	wCR.Name = request.WorkloadName

	updated, err := h.services.WorkloadService.UpdateWorkload(ctx, request.NamespaceName, &wCR)
	if err != nil {
		if errors.Is(err, services.ErrForbidden) {
			return gen.UpdateWorkload403JSONResponse{ForbiddenJSONResponse: forbidden()}, nil
		}
		if errors.Is(err, workloadsvc.ErrWorkloadNotFound) {
			return gen.UpdateWorkload404JSONResponse{NotFoundJSONResponse: notFound("Workload")}, nil
		}
		if validationErr, ok := errors.AsType[*services.ValidationError](err); ok {
			if validationErr.StatusCode == http.StatusUnprocessableEntity {
				return gen.UpdateWorkload422JSONResponse{UnprocessableContentJSONResponse: unprocessableContent(validationErr.Msg)}, nil
			}
			return gen.UpdateWorkload400JSONResponse{BadRequestJSONResponse: badRequest(validationErr.Msg)}, nil
		}
		h.logger.Error("Failed to update workload", "error", err)
		return gen.UpdateWorkload500JSONResponse{InternalErrorJSONResponse: internalError()}, nil
	}

	audit.SetResource(ctx, &audit.Resource{Namespace: request.NamespaceName, UID: string(updated.UID), Name: updated.Name})

	genWorkload, err := convert[openchoreov1alpha1.Workload, gen.Workload](*updated)
	if err != nil {
		h.logger.Error("Failed to convert updated workload", "error", err)
		return gen.UpdateWorkload500JSONResponse{InternalErrorJSONResponse: internalError()}, nil
	}

	h.logger.Info("Workload updated successfully", "namespaceName", request.NamespaceName, "workload", updated.Name)
	return gen.UpdateWorkload200JSONResponse(genWorkload), nil
}

// DeleteWorkload deletes a workload by name.
func (h *Handler) DeleteWorkload(
	ctx context.Context,
	request gen.DeleteWorkloadRequestObject,
) (gen.DeleteWorkloadResponseObject, error) {
	h.logger.Info("DeleteWorkload called", "namespaceName", request.NamespaceName, "workloadName", request.WorkloadName)

	err := h.services.WorkloadService.DeleteWorkload(ctx, request.NamespaceName, request.WorkloadName)
	if err != nil {
		if errors.Is(err, services.ErrForbidden) {
			return gen.DeleteWorkload403JSONResponse{ForbiddenJSONResponse: forbidden()}, nil
		}
		if errors.Is(err, workloadsvc.ErrWorkloadNotFound) {
			return gen.DeleteWorkload404JSONResponse{NotFoundJSONResponse: notFound("Workload")}, nil
		}
		h.logger.Error("Failed to delete workload", "error", err)
		return gen.DeleteWorkload500JSONResponse{InternalErrorJSONResponse: internalError()}, nil
	}

	h.logger.Info("Workload deleted successfully", "namespaceName", request.NamespaceName, "workload", request.WorkloadName)
	return gen.DeleteWorkload204Response{}, nil
}

// ApplyWorkload creates or replaces a workload and manages the Endpoints its descriptor asks for.
func (h *Handler) ApplyWorkload(
	ctx context.Context,
	request gen.ApplyWorkloadRequestObject,
) (gen.ApplyWorkloadResponseObject, error) {
	h.logger.Info("ApplyWorkload called", "namespaceName", request.NamespaceName, "workloadName", request.WorkloadName)

	if request.Body == nil {
		return gen.ApplyWorkload400JSONResponse{BadRequestJSONResponse: badRequest("Request body is required")}, nil
	}

	wCR, err := convert[gen.Workload, openchoreov1alpha1.Workload](request.Body.Workload)
	if err != nil {
		h.logger.Error("Failed to convert apply request", "error", err)
		return gen.ApplyWorkload400JSONResponse{BadRequestJSONResponse: badRequest("Invalid request body")}, nil
	}
	wCR.Name = request.WorkloadName

	applyReq := &workloadsvc.ApplyRequest{Workload: &wCR}
	if request.Body.Exposures != nil {
		applyReq.ManageEndpoints = true
		applyReq.Exposures, err = toExposures(*request.Body.Exposures)
		if err != nil {
			return gen.ApplyWorkload400JSONResponse{BadRequestJSONResponse: badRequest(err.Error())}, nil
		}
	}

	result, err := h.applyWorkload(ctx, request.NamespaceName, applyReq)
	if err != nil {
		if errors.Is(err, services.ErrForbidden) {
			return gen.ApplyWorkload403JSONResponse{ForbiddenJSONResponse: forbidden()}, nil
		}
		if errors.Is(err, workloadsvc.ErrComponentNotFound) {
			return gen.ApplyWorkload400JSONResponse{BadRequestJSONResponse: badRequest("Referenced component not found")}, nil
		}
		if errors.Is(err, workloadsvc.ErrWorkloadAlreadyExists) {
			return gen.ApplyWorkload409JSONResponse{ConflictJSONResponse: conflict("Workload was created concurrently; retry")}, nil
		}
		if validationErr, ok := errors.AsType[*services.ValidationError](err); ok {
			switch validationErr.StatusCode {
			case http.StatusUnprocessableEntity:
				return gen.ApplyWorkload422JSONResponse{UnprocessableContentJSONResponse: unprocessableContent(validationErr.Msg)}, nil
			case http.StatusConflict:
				return gen.ApplyWorkload409JSONResponse{ConflictJSONResponse: conflict(validationErr.Msg)}, nil
			}
			return gen.ApplyWorkload400JSONResponse{BadRequestJSONResponse: badRequest(validationErr.Msg)}, nil
		}
		h.logger.Error("Failed to apply workload", "error", err)
		return gen.ApplyWorkload500JSONResponse{InternalErrorJSONResponse: internalError()}, nil
	}

	audit.SetResource(ctx, &audit.Resource{Namespace: request.NamespaceName, UID: string(result.Workload.UID), Name: result.Workload.Name})

	genWorkload, err := convert[openchoreov1alpha1.Workload, gen.Workload](*result.Workload)
	if err != nil {
		h.logger.Error("Failed to convert applied workload", "error", err)
		return gen.ApplyWorkload500JSONResponse{InternalErrorJSONResponse: internalError()}, nil
	}

	response := gen.ApplyWorkloadResponse{Workload: genWorkload}
	response.Endpoints.Created = nonNil(result.Created)
	response.Endpoints.Updated = nonNil(result.Updated)
	response.Endpoints.Replaced = nonNil(result.Replaced)
	response.Endpoints.Deleted = nonNil(result.Deleted)
	response.Endpoints.Unchanged = nonNil(result.Unchanged)
	return gen.ApplyWorkload200JSONResponse(response), nil
}

func (h *Handler) applyWorkload(ctx context.Context, namespaceName string, req *workloadsvc.ApplyRequest) (*workloadsvc.ApplyResult, error) {
	plan, err := h.services.WorkloadService.PlanApply(ctx, namespaceName, req)
	if err != nil {
		return nil, err
	}
	return h.services.WorkloadService.ApplyWorkload(ctx, namespaceName, plan)
}

func toExposures(in []gen.WorkloadExposure) ([]workloadexposure.Exposure, error) {
	exposures := make([]workloadexposure.Exposure, 0, len(in))
	for _, e := range in {
		exposure := workloadexposure.Exposure{
			Endpoint: e.Endpoint,
			Name:     ptr.Deref(e.Name, ""),
			Via:      ptr.Deref(e.Via, ""),
		}
		for _, v := range e.Visibility {
			exposure.Visibility = append(exposure.Visibility, openchoreov1alpha1.EndpointVisibility(v))
		}
		if e.Parameters != nil && len(*e.Parameters) > 0 {
			raw, err := json.Marshal(*e.Parameters)
			if err != nil {
				return nil, fmt.Errorf("invalid parameters for endpoint %q: %w", e.Endpoint, err)
			}
			exposure.Parameters = &runtime.RawExtension{Raw: raw}
		}
		exposures = append(exposures, exposure)
	}
	return exposures, nil
}

func nonNil(names []string) []string {
	if names == nil {
		return []string{}
	}
	return names
}
