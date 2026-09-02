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
	"github.com/supanadit/forge/internal/repository/disk/pkgmgr"
)

// Executor implements build.StepExecutor by dispatching each step kind to its
// handler. It composes the fetch and builder use cases (injected as services),
// uses the shared interpolation helper, and detects the OS package manager for
// packages ops.
type Executor struct {
	fetchService   *fetch.Service
	builderService *builder.Service
	cacheDir       string
	verbose        bool
	// pkgMgr is the detected OS package manager, resolved once on the first
	// packages op (or cleanup). pkgMgrErr captures a detection failure.
	pkgMgr     pkgmgr.Manager
	pkgMgrErr  error
	pkgMgrOnce sync.Once
	// pkgCleanups collects resolved package records (build/runtime/remove) for
	// the post-build cleanup lifecycle.
	cleanupMu  sync.Mutex
	pkgCleanups []pkgCleanup
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

// packageManager resolves the OS package manager once, honoring a per-build
// override (from --pkg-manager / FORGE_PKG_MANAGER). The result is cached so
// all packages ops and the final cleanup use the same manager.
func (e *Executor) packageManager(override string) (pkgmgr.Manager, error) {
	e.pkgMgrOnce.Do(func() {
		e.pkgMgr, e.pkgMgrErr = pkgmgr.Detect(override)
	})
	return e.pkgMgr, e.pkgMgrErr
}

// RunCleanup runs the package cleanup lifecycle once, aggregating the
// resolved build/runtime/remove lists across all collected packages ops. When
// noCache is true, it also removes the fetch cache (downloaded archives +
// extracted source trees) so a throwaway build leaves no build artifacts
// behind.
func (e *Executor) RunCleanup(ctx context.Context, vars map[string]string, verbose bool, noCache bool) error {
	// When no_cache is set, remove the fetch cache first so no build sources
	// remain, regardless of package cleanup outcome.
	if noCache {
		if err := os.RemoveAll(e.cacheDir); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove fetch cache %s: %w", e.cacheDir, err)
		}
	}

	e.cleanupMu.Lock()
	cleanups := make([]pkgCleanup, len(e.pkgCleanups))
	copy(cleanups, e.pkgCleanups)
	e.cleanupMu.Unlock()

	if len(cleanups) == 0 {
		return nil
	}

	mgr, err := e.packageManager("")
	if err != nil {
		return err
	}

	// Aggregate build, runtime, and remove packages across all installs,
	// de-duplicating by name.
	aggregate := func(lists [][]string) []string {
		var out []string
		seen := map[string]bool{}
		for _, list := range lists {
			for _, p := range list {
				if !seen[p] {
					seen[p] = true
					out = append(out, p)
				}
			}
		}
		return out
	}
	allBuild := aggregate([][]string{})
	allRuntime := aggregate([][]string{})
	allRemove := aggregate([][]string{})
	for _, c := range cleanups {
		allBuild = append(allBuild, c.build...)
		allRuntime = append(allRuntime, c.runtime...)
		allRemove = append(allRemove, c.remove...)
	}
	allBuild = aggregate([][]string{allBuild})
	allRuntime = aggregate([][]string{allRuntime})
	allRemove = aggregate([][]string{allRemove})

	// Run the package cleanup lifecycle once with the aggregated lists.
	return mgr.Cleanup(ctx, allBuild, allRuntime, allRemove, verbose)
}
