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
func (e *Executor) executeApt(ctx context.Context, apt *domain.AptStep, vars map[string]string, verbose bool) error {
	if apt == nil {
		return fmt.Errorf("apt step has no config")
	}
	switch apt.Action {
	case "install", "remove", "purge":
	default:
		return fmt.Errorf("unsupported apt action %q", apt.Action)
	}

	lookup := repository.EnvLookup(vars)
	var pkgs []string
	pkgs = append(pkgs, apt.Packages...)
	for _, cp := range apt.PackagesConditional {
		cond := repository.Replace(cp.Condition, lookup)
		ok, err := evalCondition(ctx, cond, varsToEnv(vars))
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
	cmd := exec.Command("apt-get", args...)
	var err error
	if verbose {
		out, e := runProcessVerbose(ctx, cmd, nil, nil)
		if e != nil {
			err = fmt.Errorf("apt-get %s %s: %w\n%s", apt.Action, strings.Join(pkgs, " "), e, out)
		}
	} else {
		var out []byte
		out, err = runProcess(ctx, cmd)
		if err != nil {
			err = fmt.Errorf("apt-get %s %s: %w\n%s", apt.Action, strings.Join(pkgs, " "), err, out)
		}
	}
	return err
}

// evalCondition evaluates a shell condition string (e.g. "17 -ge 17").
func evalCondition(ctx context.Context, cond string, env []string) (bool, error) {
	// Use bash [[ ]] with the vars as environment so ${VAR%%.*} style
	// expressions resolve. Exit 0 = true, 1 = false.
	cmd := exec.Command("bash", "-c", "[[ "+cond+" ]]")
	cmd.Env = env
	if _, err := runProcess(ctx, cmd); err != nil {
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
			return false, nil
		}
		return false, fmt.Errorf("eval condition %q: %w", cond, err)
	}
	return true, nil
}

// varsToEnv converts a vars map plus the OS environment into a KEY=VALUE slice
// so bash can resolve ${VAR%%.*}-style expressions in conditions.
func varsToEnv(vars map[string]string) []string {
	merged := map[string]string{}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			merged[k] = v
		}
	}
	for k, v := range vars {
		merged[k] = v
	}
	out := make([]string, 0, len(merged))
	for k, v := range merged {
		out = append(out, k+"="+v)
	}
	return out
}
