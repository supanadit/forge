package pkgmgr

import "sync"

// Checker is a lazy package-status checker backed by the detected manager. It
// implements the cache package's PackageChecker port structurally: an
// environment where detection fails reports every package as not installed,
// which makes package steps unverifiable so the build cache safely re-executes
// them rather than trusting a stale entry.
type Checker struct {
	once sync.Once
	mgr  Manager
	err  error
}

// NewChecker creates a checker that resolves the manager on first use.
func NewChecker() *Checker {
	return &Checker{}
}

// Installed reports whether pkg is installed on the current OS.
func (c *Checker) Installed(pkg string) bool {
	c.once.Do(func() {
		c.mgr, c.err = Detect("")
	})
	if c.err != nil || c.mgr == nil {
		return false
	}
	return c.mgr.Installed(pkg)
}