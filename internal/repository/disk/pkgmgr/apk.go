package pkgmgr

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os/exec"
	"sort"
	"strings"
)

// Apk manages packages on Alpine Linux via apk.
type Apk struct{ runner }

// ID returns the manager name.
func (*Apk) ID() string { return "apk" }

// Install installs runtime packages.
func (m *Apk) Install(ctx context.Context, pkgs []string, verbose bool) error {
	if len(pkgs) == 0 {
		return nil
	}
	return m.run(ctx, verbose, "apk", append([]string{"add", "--no-cache"}, pkgs...)...)
}

// InstallBuild installs build packages into a deterministic virtual group so
// Cleanup can remove exactly the build set (and its pulled-in deps) with a
// single `apk del`.
func (m *Apk) InstallBuild(ctx context.Context, group string, pkgs []string, verbose bool) error {
	if len(pkgs) == 0 {
		return nil
	}
	return m.run(ctx, verbose, "apk", append([]string{"add", "--no-cache", "--virtual", group}, pkgs...)...)
}

// Installed reports whether pkg is installed.
func (m *Apk) Installed(pkg string) bool {
	return exec.Command("apk", "info", "-e", pkg).Run() == nil
}

// Cleanup runs the apk end-of-build lifecycle once:
//  1. Remove the recorded build virtual groups
//  2. Remove user-specified packages
//
// The remove list is filtered to only packages that are actually installed,
// making the manifest portable across base images. Non-installed packages
// are skipped with a warning in verbose mode.
func (m *Apk) Cleanup(ctx context.Context, build, runtime, remove []string, verbose bool) error {
	if len(build) > 0 {
		if err := m.run(ctx, verbose, "apk", "del", "--purge", VirtualGroup(build)); err != nil {
			return err
		}
	}
	// Filter to only installed packages to make the list portable across base images.
	installedRemove := filterInstalled(remove, m.Installed, verbose)
	if len(installedRemove) > 0 {
		if err := m.run(ctx, verbose, "apk", append([]string{"del", "--purge"}, installedRemove...)...); err != nil {
			return err
		}
	}
	return nil
}

// VirtualGroup returns the deterministic virtual group name for a build
// package set, matching the name used at install time.
func VirtualGroup(pkgs []string) string {
	sorted := append([]string{}, pkgs...)
	sort.Strings(sorted)
	h := sha256.Sum256([]byte(strings.Join(sorted, "\x00")))
	return "forge-build-" + hex.EncodeToString(h[:8])
}