package disk

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/supanadit/forge/domain"
	"github.com/supanadit/forge/internal/repository"
)

// executeSource fetches source, builds it, and optionally installs it.
// It returns the resolved source dir and install prefix for reuse via
// `from` and `${step:NAME}` interpolation.
func (e *Executor) executeSource(ctx context.Context, src *domain.SourceStep, sctx domain.StepContext, lookup repository.Lookup) (sourceDir string, prefix string, err error) {
	if src == nil {
		return "", "", fmt.Errorf("source step has no config")
	}

	// Reuse a previous step's fetched source via the `from` field.
	if src.From != "" {
		prev, ok := sctx.Previous[src.From]
		if !ok {
			return "", "", fmt.Errorf("source step references unknown prior step %q", src.From)
		}
		sourceDir = prev.SourceDir
		prefix = prev.Prefix
	} else if src.Fetch != nil {
		buildDir := ""
		switch src.Fetch.Type {
		case domain.FetchTypeArchive:
			spec := *src.Fetch.Archive
			spec.URL = repository.Replace(spec.URL, lookup)
			spec.Dest = repository.Replace(spec.Dest, lookup)
			buildDir, err = e.fetchService.FetchArchive(ctx, spec)
		case domain.FetchTypeGit:
			spec := *src.Fetch.Git
			spec.URL = repository.Replace(spec.URL, lookup)
			spec.Dest = repository.Replace(spec.Dest, lookup)
			spec.Ref = repository.Replace(spec.Ref, lookup)
			buildDir, err = e.fetchService.FetchGit(ctx, spec, sctx.Verbose)
		default:
			return "", "", fmt.Errorf("source step: %w: %q", domain.ErrUnknownStepKind, src.Fetch.Type)
		}
		if err != nil {
			return "", "", err
		}
		sourceDir = buildDir
		if src.Build != nil {
			prefix = src.Build.Prefix
		}
	} else {
		return "", "", fmt.Errorf("source step has neither fetch nor from")
	}

	if src.Build == nil {
		return sourceDir, prefix, nil
	}

	// Subdirectory build (e.g. PostgreSQL contrib).
	buildRoot := sourceDir
	if src.Dir != "" {
		buildRoot = filepath.Join(sourceDir, src.Dir)
	}

	env := mergeMapEnv(src.Env, sctx.Vars, lookup)
	buildSpec := *src.Build
	if buildSpec.Prefix == "" {
		buildSpec.Prefix = prefix
	}
	buildSpec.Verbose = sctx.Verbose

	buildDir, err := e.builderService.Build(ctx, buildSpec, buildRoot, env)
	if err != nil {
		return sourceDir, prefix, err
	}

	if src.Install {
		if err := e.builderService.Install(ctx, buildDir, buildSpec.Prefix, buildSpec.InstallTarget, sctx.Verbose); err != nil {
			return sourceDir, prefix, err
		}
	}

	if err := e.executeVerify(src.Verify, lookup); err != nil {
		return sourceDir, prefix, err
	}

	return sourceDir, prefix, nil
}

// mergeMapEnv builds the process env for a step: start from vars, then apply
// step env (with interpolation), then the OS environment.
func mergeMapEnv(stepEnv map[string]string, vars map[string]string, lookup repository.Lookup) []string {
	merged := map[string]string{}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			merged[k] = v
		}
	}
	for k, v := range vars {
		merged[k] = v
	}
	for k, v := range stepEnv {
		merged[k] = repository.Replace(v, lookup)
	}
	var out []string
	for k, v := range merged {
		out = append(out, k+"="+v)
	}
	return out
}
