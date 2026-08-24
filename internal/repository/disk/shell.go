package disk

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/supanadit/forge/domain"
	"github.com/supanadit/forge/internal/repository"
)

// executeShell runs arbitrary shell commands in order. Unless caching is
// disabled, it snapshots the watched filesystem roots before and after the
// commands and returns the created/modified paths as the step's outputs, so
// the build cache can verify and restore the step without any manual
// cache_verify declarations.
func (e *Executor) executeShell(ctx context.Context, sh *domain.ShellStep, sctx domain.StepContext, lookup repository.Lookup) ([]string, error) {
	if sh == nil {
		return nil, fmt.Errorf("shell step has no config")
	}

	env := mergeMapEnv(sh.Env, sctx.Vars, lookup)
	dir := sh.Dir
	if dir != "" {
		dir = repository.Replace(dir, lookup)
	}

	// Snapshot before execution. The step's own working directory is added as
	// an extra root (walked without exclusions) so builds that happen inside
	// a fetched source tree are captured too.
	var before fsSnapshot
	var extra []string
	track := !sctx.NoCache
	if track {
		if dir != "" {
			extra = append(extra, dir)
		}
		before = takeSnapshot(extra...)
	}

	for _, cmdStr := range sh.Commands {
		interp := repository.Replace(cmdStr, lookup)
		// Use exec.Command (not CommandContext): runProcess owns ctx
		// cancellation and kills the whole process group, not just the shell.
		cmd := exec.Command("sh", "-c", interp)
		cmd.Env = env
		if dir != "" {
			cmd.Dir = dir
		}
		if sctx.Verbose {
			fmt.Println("  $", interp)
			out, err := runProcessVerbose(ctx, cmd, nil, nil)
			if err != nil {
				return nil, fmt.Errorf("shell %q: %w\n%s", interp, err, out)
			}
		} else {
			out, err := runProcess(ctx, cmd)
			if err != nil {
				return nil, fmt.Errorf("shell %q: %w\n%s", interp, err, out)
			}
		}
	}

	var outputs []string
	if track {
		outputs = diffSnapshots(before, takeSnapshot(extra...))
	}

	return outputs, e.executeVerify(sh.Verify, lookup)
}
