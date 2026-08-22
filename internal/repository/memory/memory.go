// Package memory provides in-memory fakes for forge's module repository
// interfaces. It is the test driver: deterministic, no I/O, used in unit
// tests and as a stand-in before the disk driver is wired.
package memory

import (
	"context"
	"errors"
	"os"

	"github.com/supanadit/forge/domain"
)

// ManifestRepository is an in-memory fake of manifest.ManifestRepository.
type ManifestRepository struct {
	manifests map[string]domain.Manifest
}

// NewManifestRepository creates an empty in-memory manifest repository.
func NewManifestRepository() *ManifestRepository {
	return &ManifestRepository{manifests: make(map[string]domain.Manifest)}
}

// Set registers a manifest under a path for Load to return.
func (r *ManifestRepository) Set(path string, m domain.Manifest) {
	r.manifests[path] = m
}

// Load returns the manifest registered at path, or ErrManifestNotFound.
func (r *ManifestRepository) Load(ctx context.Context, path string) (domain.Manifest, error) {
	m, ok := r.manifests[path]
	if !ok {
		return domain.Manifest{}, domain.ErrManifestNotFound
	}
	return m, nil
}

// FetchRepository is an in-memory fake of fetch.FetchRepository.
type FetchRepository struct {
	sourceDirs map[string]string
}

// NewFetchRepository creates an empty in-memory fetch repository.
func NewFetchRepository() *FetchRepository {
	return &FetchRepository{sourceDirs: make(map[string]string)}
}

// SetSource registers a source dir for a given key (URL).
func (r *FetchRepository) SetSource(key, dir string) {
	r.sourceDirs[key] = dir
}

// FetchArchive returns the registered source dir for the URL, or a sentinel.
func (r *FetchRepository) FetchArchive(ctx context.Context, spec domain.ArchiveFetch) (string, error) {
	if dir, ok := r.sourceDirs[spec.URL]; ok {
		return dir, nil
	}
	return "", errors.New("fetch: no source registered")
}

// FetchGit returns the registered source dir for the URL, or a sentinel.
func (r *FetchRepository) FetchGit(ctx context.Context, spec domain.GitFetch, verbose bool) (string, error) {
	if dir, ok := r.sourceDirs[spec.URL]; ok {
		return dir, nil
	}
	return "", errors.New("fetch: no source registered")
}

// BuildRepository is an in-memory fake of builder.BuildRepository.
type BuildRepository struct {
	built map[string]string // sourceDir -> buildDir
}

// NewBuildRepository creates an empty in-memory build repository.
func NewBuildRepository() *BuildRepository {
	return &BuildRepository{built: make(map[string]string)}
}

// Build returns the registered build dir for a source dir, creating a stub.
func (r *BuildRepository) Build(ctx context.Context, spec domain.BuildSpec, sourceDir string, env []string) (string, error) {
	if dir, ok := r.built[sourceDir]; ok {
		return dir, nil
	}
	return sourceDir, nil
}

// Install creates the prefix directory to simulate an install.
func (r *BuildRepository) Install(ctx context.Context, buildDir string, spec domain.BuildSpec, verbose bool) error {
	return os.MkdirAll(spec.Prefix, 0o755)
}

// StepExecutor is an in-memory fake of build.StepExecutor.
type StepExecutor struct {
	results map[string]domain.StepResult
	err     error
}

// NewStepExecutor creates an in-memory step executor.
func NewStepExecutor() *StepExecutor {
	return &StepExecutor{results: make(map[string]domain.StepResult)}
}

// SetResult registers a canned result for a step name.
func (r *StepExecutor) SetResult(name string, res domain.StepResult) {
	r.results[name] = res
}

// SetErr forces every execution to fail with err.
func (r *StepExecutor) SetErr(err error) {
	r.err = err
}

// Execute returns the canned result for the step name, or a success default.
func (r *StepExecutor) Execute(ctx context.Context, step domain.Step, sctx domain.StepContext) (domain.StepResult, error) {
	if r.err != nil {
		return domain.StepResult{}, r.err
	}
	if res, ok := r.results[step.Name]; ok {
		return res, nil
	}
	return domain.StepResult{Name: step.Name, Status: domain.StepStatusSuccess}, nil
}
