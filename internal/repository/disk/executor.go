package disk

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/supanadit/forge/builder"
	"github.com/supanadit/forge/domain"
	"github.com/supanadit/forge/fetch"
	"github.com/supanadit/forge/internal/repository"
)

// Executor implements build.StepExecutor by dispatching each step kind to its
// handler. It composes the fetch and builder use cases (injected as services)
// and uses the shared interpolation helper.
type Executor struct {
	fetchService   *fetch.Service
	builderService *builder.Service
	cacheDir       string
	verbose        bool
	// aptMu serializes apt-get calls. apt-get uses a dpkg lock file, so
	// parallel apt steps would otherwise fail with a lock-frontend conflict.
	aptMu sync.Mutex
	// aptCleanups collects apt installs with build/runtime packages for the
	// post-build cleanup lifecycle.
	aptCleanups []*domain.AptInstall
}

// NewExecutor creates a step executor.
func NewExecutor(f *fetch.Service, b *builder.Service) *Executor {
	return &Executor{
		fetchService:   f,
		builderService: b,
		cacheDir:       filepathJoinDefaultCache(),
	}
}

// Execute runs a single step and returns its result.
func (e *Executor) Execute(ctx context.Context, step domain.Step, sctx domain.StepContext) (res domain.StepResult, err error) {
	start := time.Now()
	res = domain.StepResult{
		Name:   step.Name,
		Status: domain.StepStatusSuccess,
	}
	defer func() { res.Duration = time.Since(start) }()

	lookup := repository.Lookup(func(name string) (string, bool) {
		if v, ok := sctx.Vars[name]; ok {
			return v, true
		}
		if field, ok := stepField(name, sctx); ok {
			return field, true
		}
		return "", false
	})

	// Run all ops in order.
	if err = e.executeOps(ctx, step.Ops, "", nil, sctx, lookup); err != nil {
		res.Status = domain.StepStatusFailed
		res.Err = err
		return
	}
	return
}

// filepathJoinDefaultCache mirrors the fetch cache location.
func filepathJoinDefaultCache() string {
	return filepath.Join(os.TempDir(), "forge-cache")
}

// stepField resolves a `${step:NAME.field}` reference from prior step results.
// Supported fields: source (resolved source dir) and prefix (install prefix).
func stepField(name string, sctx domain.StepContext) (string, bool) {
	const prefix = "step:"
	if !strings.HasPrefix(name, prefix) {
		return "", false
	}
	rest := strings.TrimPrefix(name, prefix)
	stepName, field, ok := strings.Cut(rest, ".")
	if !ok {
		return "", false
	}
	prev, ok := sctx.Previous[stepName]
	if !ok {
		return "", false
	}
	switch field {
	case "source":
		return prev.SourceDir, prev.SourceDir != ""
	case "prefix":
		return prev.Prefix, prev.Prefix != ""
	default:
		return "", false
	}
}

// recordAptCleanup appends an apt install to the cleanup collection when it
// declares build or runtime packages.
func (e *Executor) recordAptCleanup(apt *domain.AptInstall) {
	if apt == nil || (len(apt.Build) == 0 && len(apt.Runtime) == 0) {
		return
	}
	e.aptMu.Lock()
	e.aptCleanups = append(e.aptCleanups, apt)
	e.aptMu.Unlock()
}

// RunCleanup runs the apt cleanup lifecycle once, aggregating build/runtime
// packages across all collected apt installs. When noCache is true, it also
// removes the fetch cache (downloaded archives + extracted source trees) so a
// throwaway build leaves no build artifacts behind.
func (e *Executor) RunCleanup(ctx context.Context, vars map[string]string, verbose bool, noCache bool) error {
	// When no_cache is set, remove the fetch cache first so no build sources
	// remain, regardless of apt cleanup outcome.
	if noCache {
		if err := os.RemoveAll(e.cacheDir); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove fetch cache %s: %w", e.cacheDir, err)
		}
	}

	e.aptMu.Lock()
	cleanups := make([]*domain.AptInstall, len(e.aptCleanups))
	copy(cleanups, e.aptCleanups)
	e.aptMu.Unlock()

	// Aggregate build and runtime packages across all installs.
	var allBuild, allRuntime []string
	seen := map[string]bool{}
	for _, apt := range cleanups {
		for _, p := range apt.Build {
			if !seen[p] {
				seen[p] = true
				allBuild = append(allBuild, p)
			}
		}
	}
	seen = map[string]bool{}
	for _, apt := range cleanups {
		for _, p := range apt.Runtime {
			if !seen[p] {
				seen[p] = true
				allRuntime = append(allRuntime, p)
			}
		}
	}

	// Run the apt cleanup lifecycle once with the aggregated lists.
	return e.cleanupApt(ctx, allBuild, allRuntime, verbose)
}
