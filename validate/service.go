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
	"github.com/supanadit/forge/internal/repository/disk"
	"github.com/supanadit/forge/manifest"
	"github.com/supanadit/forge/scheduler"
)

// isHexDigest reports whether s is a non-empty, even-length hex string.
func isHexDigest(s string) bool {
	if s == "" || len(s)%2 != 0 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f', r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}

// checksumAlgoForLen infers the algorithm from a digest's hex length when the
// manifest did not name one explicitly.
func checksumAlgoForLen(n int) string {
	switch n {
	case 32:
		return "md5"
	case 64:
		return "sha256"
	default:
		return ""
	}
}

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
			if src := op.SourceInstall; src != nil && src.Checksum != "" {
				checksum := disk.NormalizeChecksum(src.Checksum)
				if !isHexDigest(checksum) {
					res.Errors = append(res.Errors, fmt.Sprintf("step %q: source_install checksum %q is not a valid digest", st.Name, src.Checksum))
				} else {
					algo := src.ChecksumType
					if algo == "" {
						algo = checksumAlgoForLen(len(checksum))
					}
					switch algo {
					case "sha256":
						if len(checksum) != 64 {
							res.Errors = append(res.Errors, fmt.Sprintf("step %q: sha256 checksum has %d hex chars, want 64", st.Name, len(checksum)))
						}
					case "md5":
						if len(checksum) != 32 {
							res.Errors = append(res.Errors, fmt.Sprintf("step %q: md5 checksum has %d hex chars, want 32", st.Name, len(checksum)))
						}
					default:
						res.Errors = append(res.Errors, fmt.Sprintf("step %q: unsupported checksum_type %q (want sha256 or md5)", st.Name, src.ChecksumType))
					}
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
