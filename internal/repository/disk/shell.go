package disk

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/supanadit/forge/domain"
	"github.com/supanadit/forge/internal/repository"
)

// executeShell runs arbitrary shell commands in order.
func (e *Executor) executeShell(ctx context.Context, sh *domain.ShellStep, sctx domain.StepContext, lookup repository.Lookup) error {
	if sh == nil {
		return fmt.Errorf("shell step has no config")
	}

	env := mergeMapEnv(sh.Env, sctx.Vars, lookup)
	dir := sh.Dir
	if dir != "" {
		dir = repository.Replace(dir, lookup)
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
		if e.verbose {
			fmt.Println("  $", interp)
			if err := runProcessVerbose(ctx, cmd); err != nil {
				return fmt.Errorf("shell: %w", err)
			}
		} else {
			out, err := runProcess(ctx, cmd)
			if err != nil {
				return fmt.Errorf("shell %q: %w\n%s", interp, err, out)
			}
		}
	}

	return e.executeVerify(sh.Verify, lookup)
}
