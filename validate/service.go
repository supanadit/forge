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
		if len(st.Ops) == 0 {
			res.Errors = append(res.Errors, fmt.Sprintf("step %q has no operations", st.Name))
		}
		for _, op := range st.Ops {
			if op.Packages != nil {
				if len(op.Packages.Build) == 0 && len(op.Packages.Runtime) == 0 && len(op.Packages.Remove) == 0 && len(op.Packages.Conditional) == 0 {
					res.Errors = append(res.Errors, fmt.Sprintf("step %q: packages op requires build, runtime, remove, or conditional packages", st.Name))
				}
			}
		}
	}

	// Dependency / cycle checks via the scheduler.
	if _, err := s.scheduler.Schedule(m.Steps); err != nil {
		res.Errors = append(res.Errors, err.Error())
	}

	return res, nil
}
