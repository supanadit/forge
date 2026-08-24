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
// build cache when a step's inputs are unchanged and its outputs still exist.
// It is transparent to the build service, so parallel level execution,
// fail-fast, and scheduling are unaffected.
//
// Cache hits are verified against automatically discovered outputs (shell
// snapshot diffs) or inferred paths (prefix / verify / copy destinations).
// When verification fails because the filesystem was reset — e.g. a Docker
// layer rebuild — the step's persisted artifact archive is restored and the
// check retried before falling back to re-execution.
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

	if cf, ok := svc.Lookup(project, step.Name, key); ok {
		if !Verify(step, cf) {
			// The filesystem may have been reset (Docker layer rebuild):
			// restore the persisted artifacts, then re-check.
			if err := svc.RestoreArtifact(project, step.Name, key); err == nil && Verify(step, cf) {
				return cachedResult(step.Name, cf, key), nil
			}
		} else {
			return cachedResult(step.Name, cf, key), nil
		}
	}

	res, err := e.exec(ctx, step, sctx)
	if err != nil {
		return res, err
	}
	res.CacheKey = key
	if res.Status == domain.StepStatusSuccess {
		outputs := res.Outputs
		_ = svc.Save(project, step.Name, key, CacheFile{
			SourceDir: res.SourceDir,
			Prefix:    res.Prefix,
			Deps:      deps,
			Outputs:   outputs,
		})
		// Persist artifacts so later runs can survive filesystem resets.
		// Best effort: persistence failures must never fail the build.
		paths := outputs
		if len(paths) == 0 {
			paths = OutputPaths(step)
		}
		_ = svc.SaveArtifact(project, step.Name, key, paths)
	}
	return res, nil
}

func cachedResult(name string, cf CacheFile, key string) domain.StepResult {
	return domain.StepResult{
		Name:      name,
		Status:    domain.StepStatusCached,
		SourceDir: cf.SourceDir,
		Prefix:    cf.Prefix,
		Outputs:   cf.Outputs,
		CacheKey:  key,
	}
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
