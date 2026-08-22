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
func (e *Executor) Execute(ctx context.Context, step domain.Step, sctx domain.StepContext) (domain.StepResult, error) {
	start := time.Now()
	res := domain.StepResult{
		Name:   step.Name,
		Status: domain.StepStatusSuccess,
	}
	defer func() { res.Duration = time.Since(start) }()

	// Resolve vars for interpolation: build options env + manifest vars, plus
	// ${step:NAME.field} references to prior step outputs.
	lookup := repository.Lookup(func(name string) (string, bool) {
		if v, ok := sctx.Vars[name]; ok {
			return v, true
		}
		if field, ok := stepField(name, sctx); ok {
			return field, true
		}
		return "", false
	})

	var err error
	switch step.Kind {
	case domain.StepKindApt:
		err = e.executeApt(ctx, step.Apt, sctx.Vars, sctx.Verbose)
	case domain.StepKindSource:
		var srcDir, prefix string
		srcDir, prefix, err = e.executeSource(ctx, step.Source, sctx, lookup)
		res.SourceDir = srcDir
		res.Prefix = prefix
	case domain.StepKindBinary:
		err = e.executeBinary(ctx, step.Binary, lookup, sctx.Verbose)
	case domain.StepKindShell:
		err = e.executeShell(ctx, step.Shell, sctx, lookup)
	case domain.StepKindVerify:
		if step.Verify != nil {
			err = e.executeVerify(step.Verify.Checks, lookup)
		} else {
			err = fmt.Errorf("verify step has no checks")
		}
	default:
		err = fmt.Errorf("%w: %q", domain.ErrUnknownStepKind, step.Kind)
	}

	if err != nil {
		res.Status = domain.StepStatusFailed
		res.Err = err
		return res, err
	}
	return res, nil
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
