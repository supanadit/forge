package disk

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/supanadit/forge/domain"
	"github.com/supanadit/forge/internal/repository"
)

// executeInstallOp runs the universal install op: apt, source, or binary.
// It returns the resolved source dir and install prefix for reuse via
// `${step:NAME}` interpolation.
func (e *Executor) executeInstallOp(ctx context.Context, inst *domain.InstallOp, sctx domain.StepContext, lookup repository.Lookup) (sourceDir string, prefix string, err error) {
	if inst == nil {
		return "", "", fmt.Errorf("install step has no config")
	}

	switch {
	case inst.Apt != nil:
		// apt install: run apt in the manifest dir.
		if err := e.executeApt(ctx, inst.Apt, sctx.Vars, sctx.Verbose); err != nil {
			return "", "", err
		}
		e.recordAptCleanup(inst.Apt)
		return "", "", nil
	case inst.Binary != nil:
		// binary install: download archive and copy files into place.
		if err := e.executeBinary(ctx, inst.Binary, lookup); err != nil {
			return "", "", err
		}
		return "", "", nil
	default:
		// source install: the existing logic, reading from inst.Source.
		src := inst.Source
		if src == nil {
			return "", "", fmt.Errorf("install step has no config")
		}
		return e.executeSourceInstall(ctx, src, sctx, lookup)
	}
}

// executeSourceInstall fetches source, builds it, and installs it.
func (e *Executor) executeSourceInstall(ctx context.Context, src *domain.SourceInstall, sctx domain.StepContext, lookup repository.Lookup) (sourceDir string, prefix string, err error) {
	// Build the fetch spec from the source install's type/source/ref.
	var fetchSpec domain.FetchSpec
	switch src.Type {
	case "archive":
		fetchSpec = domain.FetchSpec{
			Type:    domain.FetchTypeArchive,
			Archive: &domain.ArchiveFetch{URL: src.Source},
		}
	case "git":
		fetchSpec = domain.FetchSpec{
			Type: domain.FetchTypeGit,
			Git:  &domain.GitFetch{URL: src.Source, Ref: src.Ref},
		}
	default:
		return "", "", fmt.Errorf("install step: %w: %q", domain.ErrUnknownStepKind, src.Type)
	}

	buildDir := ""
	switch fetchSpec.Type {
	case domain.FetchTypeArchive:
		spec := *fetchSpec.Archive
		spec.URL = repository.Replace(spec.URL, lookup)
		spec.Dest = repository.Replace(spec.Dest, lookup)
		buildDir, err = e.fetchService.FetchArchive(ctx, spec)
	case domain.FetchTypeGit:
		spec := *fetchSpec.Git
		spec.URL = repository.Replace(spec.URL, lookup)
		spec.Dest = repository.Replace(spec.Dest, lookup)
		spec.Ref = repository.Replace(spec.Ref, lookup)
		buildDir, err = e.fetchService.FetchGit(ctx, spec, sctx.Verbose)
	default:
		return "", "", fmt.Errorf("install step: %w: %q", domain.ErrUnknownStepKind, fetchSpec.Type)
	}
	if err != nil {
		return "", "", err
	}
	sourceDir = buildDir
	prefix = src.Prefix

	buildRoot := sourceDir

	env := mergeMapEnv(src.Env, sctx.Vars, lookup)

	if err := e.executeOps(ctx, src.Before, buildRoot, env, sctx, lookup); err != nil {
		return sourceDir, prefix, err
	}

	buildSpec := domain.BuildSpec{
		Strategy:      domain.BuildStrategy(src.Strategy),
		Prefix:        src.Prefix,
		Flags:         src.Flags,
		Jobs:          src.Jobs,
		InstallTarget: src.InstallTarget,
		Verbose:       sctx.Verbose,
	}
	if buildSpec.Prefix == "" {
		buildSpec.Prefix = prefix
	}

	buildDir, err = e.builderService.Build(ctx, buildSpec, buildRoot, env)
	if err != nil {
		return sourceDir, prefix, err
	}

	if err := e.builderService.Install(ctx, buildDir, buildSpec, sctx.Verbose); err != nil {
		return sourceDir, prefix, err
	}

	if err := e.executeOps(ctx, src.After, buildRoot, env, sctx, lookup); err != nil {
		return sourceDir, prefix, err
	}

	if err := e.executeVerify(src.Verify, lookup); err != nil {
		return sourceDir, prefix, err
	}

	return sourceDir, prefix, nil
}

// runShellCommands runs each shell command in dir via `sh -c` with the given
// env. It mirrors the command-execution pattern used by executeShell.
func runShellCommands(ctx context.Context, cmds []string, dir string, env []string, verbose bool) error {
	for _, cmdStr := range cmds {
		cmd := exec.Command("sh", "-c", cmdStr)
		cmd.Env = env
		if dir != "" {
			cmd.Dir = dir
		}
		if verbose {
			fmt.Println("  $", cmdStr)
			out, err := runProcessVerbose(ctx, cmd, nil, nil)
			if err != nil {
				return fmt.Errorf("sh %q: %w\n%s", cmdStr, err, out)
			}
		} else {
			out, err := runProcess(ctx, cmd)
			if err != nil {
				return fmt.Errorf("sh %q: %w\n%s", cmdStr, err, out)
			}
		}
	}
	return nil
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
