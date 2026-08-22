package cache

import (
	"context"
	"fmt"

	"github.com/supanadit/forge/domain"
)

// StepExecutor is the subset of build.StepExecutor needed by the cache wrapper.
// It keeps the cache package decoupled from the build use case.
type StepExecutor interface {
	Execute(ctx context.Context, step domain.Step, sctx domain.StepContext) (domain.StepResult, error)
}

// CachedExecutor wraps an inner StepExecutor and restores results from the
// build cache when a step's inputs are unchanged and its output still exists.
// It is transparent to the build service, so parallel level execution,
// fail-fast, and scheduling are unaffected.
type CachedExecutor struct {
	inner StepExecutor
	cache *Service
}

// NewCachedExecutor wraps inner with a cache service.
func NewCachedExecutor(inner StepExecutor, svc *Service) *CachedExecutor {
	return &CachedExecutor{inner: inner, cache: svc}
}

// Execute runs a step, consulting the cache first.
func (e *CachedExecutor) Execute(ctx context.Context, step domain.Step, sctx domain.StepContext) (domain.StepResult, error) {
	// Verify steps and uncacheable steps always run.
	if sctx.NoCache || !Cacheable(step) {
		return e.exec(ctx, step, sctx)
	}

	svc := e.cache
	if sctx.CacheDir != "" && sctx.CacheDir != e.cache.dir {
		var err error
		svc, err = New(sctx.CacheDir)
		if err != nil {
			return e.exec(ctx, step, sctx)
		}
	}

	project := sctx.Project
	deps := SortedDeps(step, sctx.Previous)
	key := ComputeKey(step, sctx.Vars, deps)

	if cf, ok := svc.Lookup(project, step.Name, key); ok && Verify(step) {
		res := domain.StepResult{
			Name:      step.Name,
			Status:    domain.StepStatusCached,
			SourceDir: cf.SourceDir,
			Prefix:    cf.Prefix,
			CacheKey:  key,
		}
		return res, nil
	}

	res, err := e.exec(ctx, step, sctx)
	if err != nil {
		return res, err
	}
	res.CacheKey = key
	if res.Status == domain.StepStatusSuccess {
		svc.Save(project, step.Name, key, CacheFile{
			SourceDir: res.SourceDir,
			Prefix:    res.Prefix,
			Deps:      deps,
		})
	}
	return res, nil
}

func (e *CachedExecutor) exec(ctx context.Context, step domain.Step, sctx domain.StepContext) (domain.StepResult, error) {
	res, err := e.inner.Execute(ctx, step, sctx)
	if err != nil {
		return res, fmt.Errorf("step %q: %w", step.Name, err)
	}
	return res, nil
}

// Prune removes cache entries for steps that are no longer in the manifest.
// It is called once at the start of a build.
func (e *CachedExecutor) Prune(project string, validSteps map[string]bool) {
	_ = e.cache.Prune(project, validSteps)
}
