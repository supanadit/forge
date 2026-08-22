// Package manifest is the use case for loading and resolving forge manifests.
//
// It owns the ManifestRepository contract (driver-agnostic) and exposes a
// Service that loads a TOML manifest from a path, resolving includes (inline
// splice + named groups) and validating basic schema before returning a
// domain.Manifest. The repository driver is injected and swappable.
package manifest

import (
	"context"

	"github.com/supanadit/forge/domain"
)

// ManifestRepository is the contract for loading a manifest from a source
// (filesystem, embedded, remote). Implementations live under
// internal/repository/<driver>/manifest.go.
type ManifestRepository interface {
	// Load reads, parses, resolves includes, and returns the manifest at path.
	Load(ctx context.Context, path string) (domain.Manifest, error)
}

// Service is the manifest use case. It delegates to the injected repository
// and can add cross-cutting concerns (validation, caching) in the future.
type Service struct {
	repo ManifestRepository
}

// NewService creates a manifest service backed by the given repository.
func NewService(r ManifestRepository) *Service {
	return &Service{repo: r}
}

// Load returns the resolved manifest at path.
func (s *Service) Load(ctx context.Context, path string) (domain.Manifest, error) {
	return s.repo.Load(ctx, path)
}
