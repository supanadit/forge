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

// Cleanup runs the rpm-family end-of-build lifecycle once: remove build +
// remove packages, autoremove, and clean caches.
func (m *rpm) Cleanup(ctx context.Context, build, runtime, remove []string, verbose bool) error {
	toRemove := append(append([]string{}, build...), remove...)
	if len(toRemove) > 0 {
		if err := m.run(ctx, verbose, m.bin, append([]string{"remove", "-y"}, toRemove...)...); err != nil {
			return err
		}
	}
	if err := m.run(ctx, verbose, m.bin, "autoremove", "-y"); err != nil {
		return err
	}
	if err := m.run(ctx, verbose, m.bin, "clean", "all"); err != nil {
		return err
	}
	return nil
}