// Package build is the core use case: it orchestrates the full build of a
// manifest, from loading through scheduling to parallel step execution.
//
// It owns the StepExecutor contract (the seam where the driver executes a
// single step) and composes the manifest, scheduler, fetch, and builder use
// cases. It is driver-agnostic: only app/main.go knows which driver backs the
// StepExecutor interface.
package build

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/supanadit/forge/domain"
	"github.com/supanadit/forge/manifest"
	"github.com/supanadit/forge/scheduler"
)

// StepExecutor is the contract for executing a single build step.
// Implementations live under internal/repository/<driver>/executor.go.
type StepExecutor interface {
	// Execute runs a single step and returns its result.
	Execute(ctx context.Context, step domain.Step, sctx domain.StepContext) (domain.StepResult, error)
}

// CachePruner is an optional interface a StepExecutor may implement to remove
// cache entries for steps that no longer exist in the manifest.
type CachePruner interface {
	Prune(project string, validSteps map[string]bool)
}

// Service orchestrates the build pipeline.
type Service struct {
	manifest  *manifest.Service
	scheduler *scheduler.Service
	executor  StepExecutor
}

// NewService creates a build service.
func NewService(m *manifest.Service, s *scheduler.Service, e StepExecutor) *Service {
	return &Service{manifest: m, scheduler: s, executor: e}
}

// Build loads the manifest, computes the execution plan, and executes the
// steps in parallel levels while respecting depends_on ordering.
func (s *Service) Build(ctx context.Context, manifestPath string, opts domain.BuildOptions) (domain.BuildResult, error) {
	start := time.Now()

	m, err := s.manifest.Load(ctx, manifestPath)
	if err != nil {
		return domain.BuildResult{}, err
	}
	opts.NoCache = opts.NoCache || m.Project.NoCache

	plan, err := s.scheduler.Schedule(m.Steps)
	if err != nil {
		return domain.BuildResult{}, err
	}

	if opts.DryRun {
		return domain.BuildResult{Steps: dryRunResults(plan), TotalDuration: time.Since(start)}, nil
	}

	byName := make(map[string]domain.Step, len(m.Steps))
	for _, st := range m.Steps {
		byName[st.Name] = st
	}

	// Prune cache entries for steps no longer in the manifest.
	if !opts.NoCache {
		if pruner, ok := s.executor.(CachePruner); ok {
			valid := make(map[string]bool, len(byName))
			for name := range byName {
				valid[name] = true
			}
			pruner.Prune(m.Project.Name, valid)
		}
	}

	results := make([]domain.StepResult, 0, len(m.Steps))
	previous := make(map[string]domain.StepResult, len(m.Steps))
	vars := resolveVars(m.Vars, opts.VarOverrides)

	var mu sync.Mutex
	var firstErr error
	stop := false

	for _, level := range plan.Levels {
		if stop && opts.FailFast {
			break
		}

		var wg sync.WaitGroup
		levelResults := make([]domain.StepResult, len(level))
		for i, name := range level {
			wg.Add(1)
			go func(i int, name string) {
				defer wg.Done()
				step := byName[name]

				mu.Lock()
				if stop {
					mu.Unlock()
					levelResults[i] = domain.StepResult{Name: name, Status: domain.StepStatusSkipped}
					return
				}
				prevCopy := make(map[string]domain.StepResult, len(previous))
				for k, v := range previous {
					prevCopy[k] = v
				}
				mu.Unlock()

				if opts.Verbose {
					fmt.Printf("  ▶ %s\n", name)
				}
				sctx := domain.StepContext{
					Vars:     vars,
					Previous: prevCopy,
					Verbose:  opts.Verbose,
					Project:  m.Project.Name,
					CacheDir: opts.CacheDir,
					NoCache:  opts.NoCache,
				}
				res, err := s.executor.Execute(ctx, step, sctx)
				if err != nil {
					res.Status = domain.StepStatusFailed
					res.Err = err
				}
				levelResults[i] = res

				mu.Lock()
				previous[name] = res
				if res.Status == domain.StepStatusFailed {
					if firstErr == nil {
						firstErr = res.Err
					}
					stop = true
				}
				mu.Unlock()
			}(i, name)
		}
		wg.Wait()
		results = append(results, levelResults...)
	}

	if opts.FailFast && firstErr != nil {
		return domain.BuildResult{Steps: results, TotalDuration: time.Since(start)}, firstErr
	}

	if cleaner, ok := s.executor.(interface{ RunCleanup(ctx context.Context, vars map[string]string, verbose bool) error }); ok {
		if err := cleaner.RunCleanup(ctx, vars, opts.Verbose); err != nil {
			return domain.BuildResult{Steps: results, TotalDuration: time.Since(start)}, err
		}
	}

	return domain.BuildResult{Steps: results, TotalDuration: time.Since(start)}, nil
}

// dryRunResults produces pending results for every step without executing.
func dryRunResults(plan domain.ExecutionPlan) []domain.StepResult {
	res := make([]domain.StepResult, 0, len(plan.Order))
	for _, name := range plan.Order {
		res = append(res, domain.StepResult{Name: name, Status: domain.StepStatusPending})
	}
	return res
}

// resolveVars merges manifest vars, environment variables, and CLI overrides.
// Precedence (lowest → highest): manifest vars → environment → -var overrides.
func resolveVars(manifestVars map[string]string, overrides []string) map[string]string {
	out := make(map[string]string, len(manifestVars)+len(overrides))
	for k, v := range manifestVars {
		out[k] = v
	}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			out[k] = v
		}
	}
	for _, kv := range overrides {
		if k, v, ok := strings.Cut(kv, "="); ok {
			out[k] = v
		}
	}
	return out
}
