// Package pkgmgr provides OS package-manager detection and drivers for
// forge's packages op. It is driver infrastructure: it knows how to translate
// an OS-agnostic package request (build/runtime/remove) into the concrete
// commands of the detected manager (apt, dnf, yum, or apk).
package pkgmgr

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sync"
)

// Manager installs, verifies, and removes OS packages via a package manager.
type Manager interface {
	// ID returns the manager name ("apt", "dnf", "yum", or "apk").
	ID() string
	// Install installs runtime packages (kept after the build).
	Install(ctx context.Context, pkgs []string, verbose bool) error
	// InstallBuild installs build packages, grouped under a virtual group
	// name so the manager can remove exactly the build set at cleanup.
	InstallBuild(ctx context.Context, group string, pkgs []string, verbose bool) error
	// Installed reports whether a package is currently installed (used by the
	// build cache to verify package installs).
	Installed(pkg string) bool
	// Cleanup runs the end-of-build lifecycle once: spares runtime packages,
	// removes build and remove packages, and cleans caches.
	Cleanup(ctx context.Context, build, runtime, remove []string, verbose bool) error
}

// Detect resolves the package manager for the current OS. An explicit
// override (from --pkg-manager) wins, then FORGE_PKG_MANAGER; otherwise the OS
// is detected from /etc/os-release (ID, then ID_LIKE) and finally by looking
// up the manager binaries on PATH. Detection is best-effort: an unresolvable
// environment returns an error naming the supported managers.
func Detect(override string) (Manager, error) {
	if override != "" {
		return fromName(override)
	}
	if env := os.Getenv("FORGE_PKG_MANAGER"); env != "" {
		return fromName(env)
	}
	id, idLike := osReleaseInfo()
	if m, ok := fromOSRelease(id, idLike); ok {
		return m, nil
	}
	for _, candidate := range []string{"apt", "dnf", "yum", "apk"} {
		if _, err := exec.LookPath(candidate); err == nil {
			return fromName(candidate)
		}
	}
	return nil, fmt.Errorf("no supported package manager found on this OS (apt/dnf/yum/apk)")
}

// fromName returns a manager by its canonical name.
func fromName(name string) (Manager, error) {
	switch name {
	case "apt":
		return &Apt{}, nil
	case "dnf":
		return newDNF(), nil
	case "yum":
		return newYum(), nil
	case "apk":
		return &Apk{}, nil
	default:
		return nil, fmt.Errorf("unsupported package manager %q (apt/dnf/yum/apk)", name)
	}
}

// fromOSRelease maps an os-release ID or ID_LIKE to a manager.
func fromOSRelease(id, idLike string) (Manager, bool) {
	switch id {
	case "debian", "ubuntu":
		return &Apt{}, true
	case "alpine":
		return &Apk{}, true
	case "fedora", "rhel", "centos", "rocky", "almalinux", "ol":
		return newDNF(), true
	case "amzn":
		return newYum(), true
	}
	switch idLike {
	case "debian":
		return &Apt{}, true
	case "rhel", "fedora":
		return newDNF(), true
	}
	return nil, false
}

// base exec helper shared by the manager drivers. It runs a command with the
// given args, serializing concurrent calls through mu (package managers hold
// their own lock files, so parallel invocation would conflict).
type runner struct {
	mu sync.Mutex
}

// run executes cmd with args, streaming output in verbose mode. It keeps the
// last tail of output so failures are reported with context.
func (r *runner) run(ctx context.Context, verbose bool, cmd string, args ...string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	c := exec.Command(cmd, args...)
	if verbose {
		fmt.Printf("  $ %s %s\n", cmd, joinArgs(args))
		out, err := runVerbose(ctx, c)
		if err != nil {
			return fmt.Errorf("%s %s: %w\n%s", cmd, joinArgs(args), err, out)
		}
		return nil
	}
	out, err := runQuiet(ctx, c)
	if err != nil {
		return fmt.Errorf("%s %s: %w\n%s", cmd, joinArgs(args), err, out)
	}
	return nil
}

func joinArgs(args []string) string {
	if len(args) == 0 {
		return ""
	}
	s := ""
	for i, a := range args {
		if i > 0 {
			s += " "
		}
		s += a
	}
	return s
}