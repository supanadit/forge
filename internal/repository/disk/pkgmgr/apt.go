package pkgmgr

import (
	"context"
	"fmt"
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

// Cleanup runs the apt end-of-build lifecycle once:
//  1. Reinstall runtime packages (marks them manually-installed so autoremove spares them)
//  2. Remove build packages
//  3. Autoremove orphaned dependencies
//  4. Clean apt caches and lists
//  5. Remove user-specified packages (last, so apt/apt-get remain available for steps 2-4)
//  6. Purge essential packages (apt/apt-utils) via dpkg, since apt refuses to
//     remove them with --allow-remove-essential
//
// The remove list is filtered to only packages that are actually installed,
// making the manifest portable across base images. Non-installed packages
// are skipped with a warning in verbose mode.
func (m *Apt) Cleanup(ctx context.Context, build, runtime, remove []string, verbose bool) error {
	// Step 1: Reinstall runtime packages to mark them as manually installed.
	if len(runtime) > 0 {
		if err := m.Install(ctx, runtime, verbose); err != nil {
			return err
		}
	}

	// Step 2: Remove build packages.
	if len(build) > 0 {
		if err := m.run(ctx, verbose, "apt-get", removeArgs(build)...); err != nil {
			return err
		}
	}

	// Step 3: Autoremove orphaned dependencies.
	if err := m.run(ctx, verbose, "apt-get", "autoremove", "--purge", "-y"); err != nil {
		return err
	}

	// Step 4: Clean apt caches and lists.
	if err := m.run(ctx, verbose, "apt-get", "clean"); err != nil {
		return err
	}
	for _, p := range []string{"/var/lib/apt/lists", "/var/cache/apt/archives"} {
		if err := os.RemoveAll(p); err != nil && !os.IsNotExist(err) {
			return err
		}
	}

	// Step 5: Remove user-specified packages via apt-get.
	// Filter to only installed packages to make the list portable across base images.
	installedRemove := filterInstalled(remove, m.Installed, verbose)
	if len(installedRemove) > 0 {
		// Separate essential packages (apt, apt-utils) which apt-get refuses to
		// remove without --allow-remove-essential. We purge those with dpkg
		// after apt-get succeeds.
		var regular, essential []string
		for _, pkg := range installedRemove {
			if isEssentialAptPkg(pkg) {
				essential = append(essential, pkg)
			} else {
				regular = append(regular, pkg)
			}
		}

		// Remove non-essential packages first via apt-get.
		if len(regular) > 0 {
			if err := m.run(ctx, verbose, "apt-get", removeArgs(regular)...); err != nil {
				return err
			}
		}

		// Step 6: Purge essential packages via dpkg. apt-get refuses to remove
		// them even with --allow-remove-essential, so we bypass apt and call
		// dpkg directly. This is safe because apt is no longer needed after
		// the previous cleanup steps.
		for _, pkg := range essential {
			if verbose {
				fmt.Fprintf(os.Stderr, "warning: purging essential package %q via dpkg\n", pkg)
			}
			if err := m.run(ctx, verbose, "dpkg", "--purge", pkg); err != nil {
				return err
			}
		}
	}

	return nil
}

// isEssentialAptPkg reports whether pkg is an essential apt package that
// apt-get refuses to remove without --allow-remove-essential. These must be
// purged via dpkg after apt is no longer needed.
func isEssentialAptPkg(pkg string) bool {
	switch pkg {
	case "apt", "apt-utils":
		return true
	}
	return false
}

// filterInstalled filters a package list to only those that are currently installed.
// Non-installed packages are skipped with a warning in verbose mode.
func filterInstalled(pkgs []string, installed func(string) bool, verbose bool) []string {
	if len(pkgs) == 0 {
		return nil
	}
	result := make([]string, 0, len(pkgs))
	for _, pkg := range pkgs {
		if installed(pkg) {
			result = append(result, pkg)
		} else if verbose {
			fmt.Fprintf(os.Stderr, "warning: package %q not installed, skipping removal\n", pkg)
		}
	}
	return result
}

func installArgs(pkgs []string) []string {
	return append([]string{"install", "-y"}, pkgs...)
}

func removeArgs(pkgs []string) []string {
	return append([]string{"remove", "-y"}, pkgs...)
}