// Package validate is a pure use case that checks a manifest is well-formed
// without executing any of its steps.
//
// It has no repository of its own: it composes the manifest use case to load
// the resolved manifest, then runs pure validation logic (schema checks,
// dependency resolution, cycle detection) reusing the scheduler use case.
package validate

import (
	"context"
	"fmt"

	"github.com/supanadit/forge/domain"
	"github.com/supanadit/forge/manifest"
	"github.com/supanadit/forge/scheduler"
)

// Service validates manifests without executing them.
type Service struct {
	manifest  *manifest.Service
	scheduler *scheduler.Service
}

// NewService creates a validate service.
func NewService(m *manifest.Service, s *scheduler.Service) *Service {
	return &Service{manifest: m, scheduler: s}
}

// Validate loads the manifest at path and checks it for structural problems.
func (s *Service) Validate(ctx context.Context, path string) (domain.ValidationResult, error) {
	m, err := s.manifest.Load(ctx, path)
	if err != nil {
		return domain.ValidationResult{}, err
	}

	res := domain.ValidationResult{
		StepCount:    len(m.Steps),
		IncludeCount: len(m.Includes),
	}

	// Schema checks per step.
	for _, st := range m.Steps {
		if st.Name == "" {
			res.Errors = append(res.Errors, "step has an empty name")
		}
		switch st.Kind {
		case domain.StepKindApt:
			if st.Apt == nil {
				res.Errors = append(res.Errors, fmt.Sprintf("step %q is kind %q but has no apt config", st.Name, st.Kind))
			} else if st.Apt.Action == "" {
				res.Errors = append(res.Errors, fmt.Sprintf("apt step %q has no action (install/remove/purge)", st.Name))
			}
		case domain.StepKindSource:
			if st.Source == nil {
				res.Errors = append(res.Errors, fmt.Sprintf("step %q is kind %q but has no source config", st.Name, st.Kind))
			} else if st.Source.Fetch == nil && st.Source.From == "" {
				res.Errors = append(res.Errors, fmt.Sprintf("source step %q must declare fetch or from", st.Name))
			} else if st.Source.Fetch != nil && st.Source.Fetch.Type == "" {
				res.Errors = append(res.Errors, fmt.Sprintf("source step %q has no fetch type", st.Name))
			}
		case domain.StepKindBinary:
			if st.Binary == nil {
				res.Errors = append(res.Errors, fmt.Sprintf("step %q is kind %q but has no binary config", st.Name, st.Kind))
			} else if st.Binary.Fetch == nil {
				res.Errors = append(res.Errors, fmt.Sprintf("binary step %q has no fetch config", st.Name))
			}
		case domain.StepKindShell:
			if st.Shell == nil {
				res.Errors = append(res.Errors, fmt.Sprintf("step %q is kind %q but has no shell config", st.Name, st.Kind))
			} else if len(st.Shell.Commands) == 0 {
				res.Errors = append(res.Errors, fmt.Sprintf("shell step %q has no commands", st.Name))
			}
		case domain.StepKindVerify:
			if st.Verify == nil || len(st.Verify.Checks) == 0 {
				res.Errors = append(res.Errors, fmt.Sprintf("verify step %q has no checks", st.Name))
			}
		default:
			res.Errors = append(res.Errors, fmt.Sprintf("step %q has unsupported kind %q", st.Name, st.Kind))
		}
	}

	// Dependency / cycle checks via the scheduler.
	if _, err := s.scheduler.Schedule(m.Steps); err != nil {
		res.Errors = append(res.Errors, err.Error())
	}

	return res, nil
}
