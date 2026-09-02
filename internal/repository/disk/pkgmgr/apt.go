package pkgmgr

import (
	"context"
	"os"
	"os/exec"
	"strings"
)

// Apt manages packages on Debian/Ubuntu via apt-get.
type Apt struct{ runner }

// ID returns the manager name.
func (*Apt) ID() string { return "apt" }

// Install installs runtime packages.
func (m *Apt) Install(ctx context.Context, pkgs []string, verbose bool) error {
	if len(pkgs) == 0 {
		return nil
	}
	return m.run(ctx, verbose, "apt-get", installArgs(pkgs)...)
}

// InstallBuild installs build packages. apt has no virtual groups, so the
// group name is ignored; cleanup removes the build list explicitly.
func (m *Apt) InstallBuild(ctx context.Context, _ string, pkgs []string, verbose bool) error {
	return m.Install(ctx, pkgs, verbose)
}

// Installed reports whether pkg is installed via dpkg's package database.
func (m *Apt) Installed(pkg string) bool {
	out, err := exec.Command("dpkg", "-s", pkg).CombinedOutput()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "Status: install ok installed")
}

// Cleanup runs the apt end-of-build lifecycle once: reinstall runtime (marks
// them manually-installed so autoremove spares them), remove build + remove
// packages, autoremove --purge, clean, and drop apt lists and archives.
func (m *Apt) Cleanup(ctx context.Context, build, runtime, remove []string, verbose bool) error {
	if len(runtime) > 0 {
		if err := m.Install(ctx, runtime, verbose); err != nil {
			return err
		}
	}
	toRemove := append(append([]string{}, build...), remove...)
	if len(toRemove) > 0 {
		if err := m.run(ctx, verbose, "apt-get", removeArgs(toRemove)...); err != nil {
			return err
		}
	}
	if err := m.run(ctx, verbose, "apt-get", "autoremove", "--purge", "-y"); err != nil {
		return err
	}
	if err := m.run(ctx, verbose, "apt-get", "clean"); err != nil {
		return err
	}
	for _, p := range []string{"/var/lib/apt/lists", "/var/cache/apt/archives"} {
		if err := os.RemoveAll(p); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func installArgs(pkgs []string) []string {
	return append([]string{"install", "-y"}, pkgs...)
}

func removeArgs(pkgs []string) []string {
	return append([]string{"remove", "-y"}, pkgs...)
}