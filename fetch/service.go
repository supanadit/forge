// Package fetch is the use case for acquiring source code.
//
// It owns the FetchRepository contract (driver-agnostic) and exposes a Service
// that fetches a git repository or a compressed archive into a local source
// directory. The repository driver is injected and swappable.
package fetch

import (
	"context"

	"github.com/supanadit/forge/domain"
)

// FetchRepository is the contract for acquiring source trees.
// Implementations live under internal/repository/<driver>/fetch.go.
type FetchRepository interface {
	// FetchArchive downloads and extracts a compressed archive, returning the
	// resolved source directory.
	FetchArchive(ctx context.Context, spec domain.ArchiveFetch) (sourceDir string, err error)
	// FetchGit clones a git repository, returning the resolved source directory.
	FetchGit(ctx context.Context, spec domain.GitFetch, verbose bool) (sourceDir string, err error)
}

// Service is the fetch use case. It delegates to the injected repository.
type Service struct {
	repo FetchRepository
}

// NewService creates a fetch service backed by the given repository.
func NewService(r FetchRepository) *Service {
	return &Service{repo: r}
}

// FetchArchive downloads and extracts an archive, returning the source dir.
func (s *Service) FetchArchive(ctx context.Context, spec domain.ArchiveFetch) (string, error) {
	return s.repo.FetchArchive(ctx, spec)
}

// FetchGit clones a git repository, returning the source dir.
func (s *Service) FetchGit(ctx context.Context, spec domain.GitFetch, verbose bool) (string, error) {
	return s.repo.FetchGit(ctx, spec, verbose)
}
