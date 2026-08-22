// Package builder is the use case for compiling source trees.
//
// It owns the BuildRepository contract (driver-agnostic) and exposes a Service
// that compiles a source directory using a build strategy (configure, cmake,
// meson, make, autogen) and optionally installs it into a prefix. The
// repository driver is injected and swappable.
package builder

import (
	"context"

	"github.com/supanadit/forge/domain"
)

// BuildRepository is the contract for compiling and installing source trees.
// Implementations live under internal/repository/<driver>/builder.go.
type BuildRepository interface {
	// Build compiles the source at sourceDir according to spec, returning the
	// build directory.
	Build(ctx context.Context, spec domain.BuildSpec, sourceDir string, env []string) (buildDir string, err error)
	// Install installs a previously built tree into prefix. installTarget
	// overrides the make target (e.g. "altinstall"); empty runs "make install".
	Install(ctx context.Context, buildDir string, prefix string, installTarget string, verbose bool) error
}

// Service is the builder use case. It delegates to the injected repository.
type Service struct {
	repo BuildRepository
}

// NewService creates a builder service backed by the given repository.
func NewService(r BuildRepository) *Service {
	return &Service{repo: r}
}

// Build compiles the source at sourceDir according to spec.
func (s *Service) Build(ctx context.Context, spec domain.BuildSpec, sourceDir string, env []string) (string, error) {
	return s.repo.Build(ctx, spec, sourceDir, env)
}

// Install installs a previously built tree into prefix.
func (s *Service) Install(ctx context.Context, buildDir string, prefix string, installTarget string, verbose bool) error {
	return s.repo.Install(ctx, buildDir, prefix, installTarget, verbose)
}
