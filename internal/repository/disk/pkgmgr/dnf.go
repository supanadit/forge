package pkgmgr

import (
	"context"
	"os/exec"
	"strings"
)

// rpm manages packages on the Red Hat family (dnf on Fedora/RHEL/Rocky/Alma,
// yum on older CentOS/Amazon Linux).
type rpm struct {
	runner
	bin string
}

// DNF manages packages via dnf.
type DNF struct{ rpm }

// Yum manages packages via yum.
type Yum struct{ rpm }

func newDNF() *DNF   { return &DNF{rpm{bin: "dnf"}} }
func newYum() *Yum   { return &Yum{rpm{bin: "yum"}} }

// ID returns the manager name.
func (m *DNF) ID() string { return "dnf" }

// ID returns the manager name.
func (m *Yum) ID() string { return "yum" }

// Install installs runtime packages. Explicit installs are marked
// user-installed by dnf/yum, so autoremove spares them.
func (m *rpm) Install(ctx context.Context, pkgs []string, verbose bool) error {
	if len(pkgs) == 0 {
		return nil
	}
	return m.run(ctx, verbose, m.bin, append([]string{"install", "-y"}, pkgs...)...)
}

// InstallBuild installs build packages. The group name is ignored (removal is
// handled by the explicit remove list in Cleanup).
func (m *rpm) InstallBuild(ctx context.Context, _ string, pkgs []string, verbose bool) error {
	return m.Install(ctx, pkgs, verbose)
}

// Installed reports whether pkg is installed via the RPM database.
func (m *rpm) Installed(pkg string) bool {
	if err := exec.Command("rpm", "-q", pkg).Run(); err == nil {
		return true
	}
	// Query the binary's manager as a fallback for non-rpm-aligned packages.
	out, err := exec.Command(m.bin, "list", "--installed", pkg).CombinedOutput()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), pkg)
}

// Cleanup runs the rpm-family end-of-build lifecycle once:
//  1. Remove build packages
//  2. Autoremove orphaned dependencies
//  3. Clean package manager caches
//  4. Remove user-specified packages (last, so the package manager remains available for steps 1-3)
//
// The remove list is filtered to only packages that are actually installed,
// making the manifest portable across base images. Non-installed packages
// are skipped with a warning in verbose mode.
func (m *rpm) Cleanup(ctx context.Context, build, runtime, remove []string, verbose bool) error {
	// Step 1: Remove build packages.
	if len(build) > 0 {
		if err := m.run(ctx, verbose, m.bin, append([]string{"remove", "-y"}, build...)...); err != nil {
			return err
		}
	}

	// Step 2: Autoremove orphaned dependencies.
	if err := m.run(ctx, verbose, m.bin, "autoremove", "-y"); err != nil {
		return err
	}

	// Step 3: Clean package manager caches.
	if err := m.run(ctx, verbose, m.bin, "clean", "all"); err != nil {
		return err
	}

	// Step 4: Remove user-specified packages (last, so the package manager is still available).
	// Filter to only installed packages to make the list portable across base images.
	installedRemove := filterInstalled(remove, m.Installed, verbose)
	if len(installedRemove) > 0 {
		if err := m.run(ctx, verbose, m.bin, append([]string{"remove", "-y"}, installedRemove...)...); err != nil {
			return err
		}
	}

	return nil
}