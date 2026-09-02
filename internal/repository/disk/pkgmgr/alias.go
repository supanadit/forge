package pkgmgr

import (
	"fmt"
	"os"
	"strings"
)

// aliases maps a common meta-package name to its concrete per-manager
// equivalent. These packages have genuinely different names across distros;
// everything else passes through unchanged (curl, git, make, ... share names).
var aliases = map[string]map[string][]string{
	"build-essential": {
		"apt": {"build-essential"},
		"dnf": {"gcc", "gcc-c++", "make"},
		"yum": {"gcc", "gcc-c++", "make"},
		"apk": {"build-base"},
	},
	"pkg-config": {
		"apt": {"pkg-config"},
		"dnf": {"pkgconf-pkg-config"},
		"yum": {"pkgconfig"},
		"apk": {"pkgconf"},
	},
	"ca-certificates": {
		"apt": {"ca-certificates"},
		"dnf": {"ca-certificates"},
		"yum": {"ca-certificates"},
		"apk": {"ca-certificates"},
	},
}

// ResolveNames maps package names onto the detected manager's package set.
// Known meta-packages use the alias table; unknown names pass through after
// applying the -dev/-devel suffix convention for the dnf/yum family. When a
// substitution changes a name, a warning is printed so users can author
// distro-exact names to silence it.
func ResolveNames(mgr string, pkgs []string) []string {
	out := make([]string, 0, len(pkgs))
	for _, p := range pkgs {
		if mapped, ok := aliases[p]; ok {
			if names, ok := mapped[mgr]; ok {
				for _, n := range names {
					if !contains(out, n) {
						out = append(out, n)
					}
				}
				warnAlias(p, names, mgr)
				continue
			}
		}
		resolved := resolveSuffix(mgr, p)
		if !contains(out, resolved) {
			out = append(out, resolved)
		}
		if resolved != p {
			fmt.Fprintf(os.Stderr, "warning: package %q resolved to %q for %s\n", p, resolved, mgr)
		}
	}
	return out
}

// resolveSuffix normalizes the -dev/-devel convention: dnf/yum expect
// libfoo-devel where apt/apk use libfoo-dev.
func resolveSuffix(mgr, pkg string) string {
	switch mgr {
	case "dnf", "yum":
		if strings.HasSuffix(pkg, "-dev") {
			return strings.TrimSuffix(pkg, "-dev") + "-devel"
		}
	default:
		if strings.HasSuffix(pkg, "-devel") {
			return strings.TrimSuffix(pkg, "-devel") + "-dev"
		}
	}
	return pkg
}

func warnAlias(pkg string, names []string, mgr string) {
	fmt.Fprintf(os.Stderr, "warning: package %q mapped to %q for %s\n", pkg, strings.Join(names, ", "), mgr)
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}