package disk

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/supanadit/forge/domain"
	"github.com/supanadit/forge/internal/repository/disk/pkgmgr"
)

// executePackages installs the packages op through the detected OS package
// manager. Build packages go into a virtual group (so the manager can remove
// exactly that set at cleanup); runtime packages are kept. The resolved
// package lists are recorded so the end-of-build cleanup removes the same
// (alias-resolved) names that were installed.
func (e *Executor) executePackages(ctx context.Context, p *domain.PackagesOp, sctx domain.StepContext) error {
	if p == nil {
		return fmt.Errorf("packages op has no config")
	}

	build, runtime, err := packagesInstallLists(p, sctx.Vars)
	if err != nil {
		return err
	}
	// Nothing to install or clean up — no need to even detect a manager.
	if len(build) == 0 && len(runtime) == 0 && len(p.Remove) == 0 {
		return nil
	}

	mgr, err := e.packageManager(sctx.PkgManager)
	if err != nil {
		return err
	}

	build = pkgmgr.ResolveNames(mgr.ID(), build)
	runtime = pkgmgr.ResolveNames(mgr.ID(), runtime)
	remove := pkgmgr.ResolveNames(mgr.ID(), p.Remove)

	if len(build) > 0 {
		if err := mgr.InstallBuild(ctx, pkgmgr.VirtualGroup(build), build, sctx.Verbose); err != nil {
			return err
		}
	}
	if len(runtime) > 0 {
		if err := mgr.Install(ctx, runtime, sctx.Verbose); err != nil {
			return err
		}
	}
	e.recordPkgCleanup(build, runtime, remove)
	return nil
}

// pkgCleanup is a resolved package-install record awaiting end-of-build
// cleanup. Lists hold alias-resolved names so cleanup removes the exact
// packages the manager installed.
type pkgCleanup struct {
	build   []string
	runtime []string
	remove  []string
}

// recordPkgCleanup appends a resolved package record to the cleanup
// collection when it declares anything to manage.
func (e *Executor) recordPkgCleanup(build, runtime, remove []string) {
	if len(build) == 0 && len(runtime) == 0 && len(remove) == 0 {
		return
	}
	e.cleanupMu.Lock()
	e.pkgCleanups = append(e.pkgCleanups, pkgCleanup{build: build, runtime: runtime, remove: remove})
	e.cleanupMu.Unlock()
}

// packagesInstallLists computes the build and runtime package lists: the
// declared lists plus any conditional packages whose structured condition is
// true, classified by their category.
func packagesInstallLists(p *domain.PackagesOp, vars map[string]string) (build, runtime []string, err error) {
	if p == nil {
		return nil, nil, nil
	}
	build = append(build, p.Build...)
	runtime = append(runtime, p.Runtime...)
	for _, c := range p.Conditional {
		value, ok := vars[c.When.Var]
		if !ok {
			return nil, nil, fmt.Errorf("package conditional references unknown var %q", c.When.Var)
		}
		if !evalVersionCondition(c.When, value) {
			continue
		}
		if c.Category == "build" {
			build = append(build, c.Packages...)
		} else {
			runtime = append(runtime, c.Packages...)
		}
	}
	return build, runtime, nil
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