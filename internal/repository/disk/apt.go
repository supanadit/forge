package disk

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/supanadit/forge/domain"
)

// executeApt installs the apt install's build, runtime, and any conditional
// packages whose condition is true.
func (e *Executor) executeApt(ctx context.Context, apt *domain.AptInstall, vars map[string]string, verbose bool) error {
	pkgs, err := aptInstallList(apt, vars)
	if err != nil {
		return err
	}
	if len(pkgs) == 0 {
		return nil
	}
	return e.aptInstall(ctx, pkgs, verbose)
}

// aptInstallList computes the full list of packages to install: build +
// runtime + any conditional packages whose structured condition is true.
func aptInstallList(apt *domain.AptInstall, vars map[string]string) ([]string, error) {
	if apt == nil {
		return nil, nil
	}
	var pkgs []string
	pkgs = append(pkgs, apt.Build...)
	pkgs = append(pkgs, apt.Runtime...)
	for _, c := range apt.Conditional {
		value, ok := vars[c.When.Var]
		if !ok {
			return nil, fmt.Errorf("apt conditional references unknown var %q", c.When.Var)
		}
		if evalVersionCondition(c.When, value) {
			pkgs = append(pkgs, c.Packages...)
		}
	}
	return pkgs, nil
}

// aptInstall runs `apt-get install -y` for the given packages.
func (e *Executor) aptInstall(ctx context.Context, pkgs []string, verbose bool) error {
	return e.runApt(ctx, "install", pkgs, verbose)
}

// aptRemove runs `apt-get remove -y` for the given packages.
func (e *Executor) aptRemove(ctx context.Context, pkgs []string, verbose bool) error {
	return e.runApt(ctx, "remove", pkgs, verbose)
}

// runApt runs an apt-get action against the given packages.
func (e *Executor) runApt(ctx context.Context, action string, pkgs []string, verbose bool) error {
	args := []string{action, "-y"}
	args = append(args, pkgs...)
	cmd := exec.Command("apt-get", args...)
	// Serialize apt-get across parallel steps: apt uses a dpkg lock file, so
	// concurrent invocations would fail with a lock-frontend conflict.
	e.aptMu.Lock()
	defer e.aptMu.Unlock()
	var err error
	if verbose {
		out, e := runProcessVerbose(ctx, cmd, nil, nil)
		if e != nil {
			err = fmt.Errorf("apt-get %s %s: %w\n%s", action, strings.Join(pkgs, " "), e, out)
		}
	} else {
		var out []byte
		out, err = runProcess(ctx, cmd)
		if err != nil {
			err = fmt.Errorf("apt-get %s %s: %w\n%s", action, strings.Join(pkgs, " "), err, out)
		}
	}
	return err
}

// cleanupApt removes build packages, reinstalls runtime, and cleans apt caches.
// It is called once after all components finish, with aggregated lists.
func (e *Executor) cleanupApt(ctx context.Context, build, runtime []string, verbose bool) error {
	// 1. Reinstall runtime packages (marks them manually-installed so
	// autoremove keeps them).
	if len(runtime) > 0 {
		if err := e.aptInstall(ctx, runtime, verbose); err != nil {
			return err
		}
	}
	// 2. Remove build packages.
	if len(build) > 0 {
		if err := e.aptRemove(ctx, build, verbose); err != nil {
			return err
		}
	}
	// 3. autoremove --purge, clean, remove apt lists and caches.
	if err := e.runAptRaw(ctx, []string{"autoremove", "--purge", "-y"}, verbose); err != nil {
		return err
	}
	if err := e.runAptRaw(ctx, []string{"clean"}, verbose); err != nil {
		return err
	}
	for _, p := range []string{"/var/lib/apt/lists", "/var/cache/apt/archives"} {
		if err := removeAll(ctx, p, verbose); err != nil {
			return err
		}
	}
	return nil
}

// runAptRaw runs an apt-get command with the given args (no package list).
func (e *Executor) runAptRaw(ctx context.Context, args []string, verbose bool) error {
	cmd := exec.Command("apt-get", args...)
	e.aptMu.Lock()
	defer e.aptMu.Unlock()
	var err error
	if verbose {
		out, e := runProcessVerbose(ctx, cmd, nil, nil)
		if e != nil {
			err = fmt.Errorf("apt-get %s: %w\n%s", strings.Join(args, " "), e, out)
		}
	} else {
		var out []byte
		out, err = runProcess(ctx, cmd)
		if err != nil {
			err = fmt.Errorf("apt-get %s: %w\n%s", strings.Join(args, " "), err, out)
		}
	}
	return err
}

// removeAll removes a path (file or dir), ignoring not-exist errors.
func removeAll(ctx context.Context, path string, verbose bool) error {
	if verbose {
		fmt.Println("  $ rm -rf", path)
	}
	if err := os.RemoveAll(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove %s: %w", path, err)
	}
	return nil
}

// evalVersionCondition returns true if the condition holds for the given value.
func evalVersionCondition(c domain.VersionCondition, value string) bool {
	switch {
	case c.Gte != "":
		return versionCompare(value, c.Gte) >= 0
	case c.Lte != "":
		return versionCompare(value, c.Lte) <= 0
	case c.Gt != "":
		return versionCompare(value, c.Gt) > 0
	case c.Lt != "":
		return versionCompare(value, c.Lt) < 0
	case c.Eq != "":
		return versionCompare(value, c.Eq) == 0
	}
	return false
}

// versionCompare compares two version strings, returning -1, 0, or 1. It strips
// a leading "v"/"V", splits on ".", and compares each segment numerically when
// possible, falling back to string comparison for non-numeric segments. Missing
// trailing components are treated as 0.
func versionCompare(a, b string) int {
	as := versionSegments(a)
	bs := versionSegments(b)
	n := len(as)
	if len(bs) > n {
		n = len(bs)
	}
	for i := 0; i < n; i++ {
		var av, bv string
		if i < len(as) {
			av = as[i]
		}
		if i < len(bs) {
			bv = bs[i]
		}
		c := compareSegment(av, bv)
		if c != 0 {
			return c
		}
	}
	return 0
}

// versionSegments strips a leading v/V and splits a version string on ".".
func versionSegments(s string) []string {
	s = strings.TrimPrefix(s, "v")
	s = strings.TrimPrefix(s, "V")
	return strings.Split(s, ".")
}

// compareSegment compares two numeric-or-string segments. Both are compared
// numerically when each parses as an integer; otherwise lexically. A missing
// segment (empty) is treated as 0 to satisfy "missing components are 0".
func compareSegment(a, b string) int {
	if a == "" {
		a = "0"
	}
	if b == "" {
		b = "0"
	}
	ai, aerr := strconv.Atoi(a)
	bi, berr := strconv.Atoi(b)
	if aerr == nil && berr == nil {
		switch {
		case ai < bi:
			return -1
		case ai > bi:
			return 1
		default:
			return 0
		}
	}
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}
