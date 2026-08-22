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

// executeApt installs/removes system packages via apt-get.
func (e *Executor) executeApt(ctx context.Context, apt *domain.AptStep, lookup repository.Lookup) error {
	if apt == nil {
		return fmt.Errorf("apt step has no config")
	}
	switch apt.Action {
	case "install", "remove", "purge":
	default:
		return fmt.Errorf("unsupported apt action %q", apt.Action)
	}

	var pkgs []string
	pkgs = append(pkgs, apt.Packages...)
	for _, cp := range apt.PackagesConditional {
		cond := repository.Replace(cp.Condition, lookup)
		ok, err := evalCondition(ctx, cond, varsToEnv(lookup))
		if err != nil {
			return err
		}
		if ok {
			pkgs = append(pkgs, cp.Packages...)
		}
	}
	if len(pkgs) == 0 {
		return nil
	}

	args := []string{apt.Action, "-y"}
	args = append(args, pkgs...)
	cmd := exec.CommandContext(ctx, "apt-get", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("apt-get %s %s: %w\n%s", apt.Action, strings.Join(pkgs, " "), err, out)
	}
	return nil
}

// evalCondition evaluates a shell condition string (e.g. "17 -ge 17").
func evalCondition(ctx context.Context, cond string, env []string) (bool, error) {
	// Use bash [[ ]] with the vars as environment so ${VAR%%.*} style
	// expressions resolve. Exit 0 = true, 1 = false.
	cmd := exec.CommandContext(ctx, "bash", "-c", "[[ "+cond+" ]]")
	cmd.Env = env
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
			return false, nil
		}
		return false, fmt.Errorf("eval condition %q: %w", cond, err)
	}
	return true, nil
}

// varsToEnv converts a Lookup into a KEY=VALUE env slice.
func varsToEnv(lookup repository.Lookup) []string {
	return os.Environ()
}
