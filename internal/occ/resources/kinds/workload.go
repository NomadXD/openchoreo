// Copyright 2025 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

package kinds

import (
	"encoding/json"
	"fmt"
	"os"

	openchoreov1alpha1 "github.com/openchoreo/openchoreo/api/v1alpha1"
	"github.com/openchoreo/openchoreo/internal/occ/resources"
	synth "github.com/openchoreo/openchoreo/internal/occ/resources/workload"
)

// WorkloadResource provides operations for Workload CRs.
type WorkloadResource struct {
	*resources.ResourceBase
}

// NewWorkloadResource constructs a WorkloadResource with CRDConfig and optionally sets namespace.
func NewWorkloadResource(cfg resources.CRDConfig, namespace string) (*WorkloadResource, error) {
	options := []resources.ResourceBaseOption{
		resources.WithResourceConfig(cfg),
	}

	if namespace != "" {
		options = append(options, resources.WithResourceNamespace(namespace))
	}

	return &WorkloadResource{
		ResourceBase: resources.NewResourceBase(options...),
	}, nil
}

// CreateWorkload creates a Workload CR from a descriptor file or basic parameters.
func (w *WorkloadResource) CreateWorkload(params synth.CreateWorkloadParams) error {
	if params.NamespaceName == "" {
		return fmt.Errorf("namespace name is required (--namespace)")
	}
	if params.ProjectName == "" {
		return fmt.Errorf("project name is required (--project)")
	}
	if params.ComponentName == "" {
		return fmt.Errorf("component name is required (--component)")
	}
	if params.ImageURL == "" {
		return fmt.Errorf("image URL is required (--image)")
	}

	var workloadCR *openchoreov1alpha1.Workload
	var exposures []synth.WorkloadExposure
	var err error

	if params.FilePath != "" {
		conversion, err := convertDescriptor(params)
		if err != nil {
			return err
		}
		workloadCR, exposures = conversion.Workload, conversion.Exposures
	} else {
		workloadCR, err = synth.CreateBasicWorkload(params)
		if err != nil {
			return fmt.Errorf("failed to create basic workload CR: %w", err)
		}
	}

	// Exposures that are not written would be lost without a trace, leaving an endpoint the
	// descriptor exposes unexposed, so a descriptor that exposes anything needs a place for them.
	if len(exposures) > 0 && params.ExposuresOutputPath == "" {
		return fmt.Errorf("the descriptor exposes %d endpoint(s); pass --exposures-output so they can be applied",
			len(exposures))
	}
	if exposures != nil && params.ExposuresOutputPath != "" {
		exposuresJSON, err := json.MarshalIndent(exposures, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to encode exposures: %w", err)
		}
		if err := os.WriteFile(params.ExposuresOutputPath, append(exposuresJSON, '\n'), 0644); err != nil { //nolint:gosec // Generated files are meant to be readable
			return fmt.Errorf("failed to write exposures file %s: %w", params.ExposuresOutputPath, err)
		}
		fmt.Fprintf(os.Stderr, "Exposures written to %s\n", params.ExposuresOutputPath)
	}

	yamlBytes, err := synth.ConvertWorkloadCRToYAML(workloadCR)
	if err != nil {
		return fmt.Errorf("failed to convert Workload CR to YAML: %w", err)
	}

	if params.OutputPath != "" {
		if err := os.WriteFile(params.OutputPath, yamlBytes, 0644); err != nil { //nolint:gosec // Generated YAML files are meant to be readable
			return fmt.Errorf("failed to write output file %s: %w", params.OutputPath, err)
		}
		fmt.Printf("Workload CR written to %s\n", params.OutputPath)
	} else {
		fmt.Print(string(yamlBytes))
	}

	return nil
}

// convertDescriptor converts params.FilePath and reports its deprecation warnings on stderr, so
// they do not mix into a Workload CR printed on stdout.
func convertDescriptor(params synth.CreateWorkloadParams) (*synth.DescriptorConversion, error) {
	conversion, err := synth.ConvertWorkloadDescriptor(params.FilePath, params)
	if err != nil {
		return nil, fmt.Errorf("failed to convert workload descriptor: %w", err)
	}
	for _, warning := range conversion.Warnings {
		fmt.Fprintf(os.Stderr, "Warning: %s\n", warning)
	}
	return conversion, nil
}

// GenerateWorkloadCR generates a Workload CR without writing it (used in file-system mode).
func (w *WorkloadResource) GenerateWorkloadCR(params synth.CreateWorkloadParams) (*openchoreov1alpha1.Workload, error) {
	if params.NamespaceName == "" {
		return nil, fmt.Errorf("namespace name is required (--namespace)")
	}
	if params.ProjectName == "" {
		return nil, fmt.Errorf("project name is required (--project)")
	}
	if params.ComponentName == "" {
		return nil, fmt.Errorf("component name is required (--component)")
	}
	if params.ImageURL == "" {
		return nil, fmt.Errorf("image URL is required (--image)")
	}

	var workloadCR *openchoreov1alpha1.Workload
	var err error

	if params.FilePath != "" {
		conversion, err := convertDescriptor(params)
		if err != nil {
			return nil, err
		}
		// File-system mode has no Endpoint generation yet; dropping the exposures would
		// silently leave the endpoints unexposed.
		if len(conversion.Exposures) > 0 {
			return nil, fmt.Errorf("the descriptor exposes %d endpoint(s), which file-system mode does not support yet",
				len(conversion.Exposures))
		}
		workloadCR = conversion.Workload
	} else {
		workloadCR, err = synth.CreateBasicWorkload(params)
		if err != nil {
			return nil, fmt.Errorf("failed to create basic workload CR: %w", err)
		}
	}

	return workloadCR, nil
}
