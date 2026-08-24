package disk

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// snapshotEntry is the minimal stat fingerprint used to detect created or
// modified paths between two snapshots. Directory mtimes are recorded but
// deliberately ignored during diffing (see diffSnapshots).
type snapshotEntry struct {
	size  int64
	mtime int64
	isDir bool
}

// Safety valves. If a diff grows past these limits the outputs are abandoned
// (nil), which simply degrades the step to uncacheable — never incorrect.
const (
	maxSnapshotEntries = 400_000
	maxOutputPaths     = 5_000
)

// defaultSnapshotRoots is the whitelist of directories watched for side
// effects of shell steps: the conventional install and config locations plus
// /opt. Deliberately excluded are high-noise, low-signal trees (/home,
// /var/lib) where dev-host churn would blow the snapshot budget; steps whose
// outputs land only there degrade to re-execution — safe, just slower.
//
// Overridable in tests to keep snapshots hermetic.
var defaultSnapshotRoots = []string{
	"/usr/local",
	"/usr/bin",
	"/usr/lib",
	"/etc",
	"/var/log",
	"/var/run",
	"/opt",
}

// excludedPrefixes are pruned during traversal of default roots: kernel
// pseudo-filesystems and volatile caches whose churn is unrelated to the
// step being snapshotted.
var excludedPrefixes = []string{
	"/proc",
	"/sys",
	"/dev",
	"/tmp",
	"/var/cache",
}

// fsSnapshot is the captured fingerprint of the watched filesystem.
type fsSnapshot struct {
	entries   map[string]snapshotEntry
	truncated bool
}

// takeSnapshot records a size+mtime fingerprint of every file and directory
// under the default roots plus any extra absolute roots (e.g. the step's
// working directory). Extra roots are walked without exclusions so a shell
// step building inside its fetched source tree still gets its outputs
// captured even when that tree lives under an excluded prefix.
func takeSnapshot(extra ...string) fsSnapshot {
	snap := fsSnapshot{entries: make(map[string]snapshotEntry, 4096)}
	for _, root := range defaultSnapshotRoots {
		walkRoot(root, true, &snap)
	}
	for _, x := range extra {
		if filepath.IsAbs(x) {
			walkRoot(filepath.Clean(x), false, &snap)
		}
	}
	return snap
}

func walkRoot(root string, applyExclusions bool, snap *fsSnapshot) {
	fi, err := os.Stat(root)
	if err != nil || !fi.IsDir() {
		return
	}
	if snap.truncated {
		return
	}
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable entries are skipped, not fatal
		}
		cleaned := filepath.Clean(path)
		if d.IsDir() && applyExclusions && cleaned != root && excludedPath(cleaned) {
			return fs.SkipDir
		}
		if _, exists := snap.entries[cleaned]; exists {
			return nil // extra root overlapping a default root: keep first entry
		}
		if len(snap.entries) >= maxSnapshotEntries {
			snap.truncated = true // abandon: an incomplete baseline yields garbage diffs
			return fs.SkipAll
		}
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		snap.entries[cleaned] = snapshotEntry{size: info.Size(), mtime: info.ModTime().UnixNano(), isDir: d.IsDir()}
		return nil
	})
}

func excludedPath(path string) bool {
	for _, ex := range excludedPrefixes {
		if path == ex || strings.HasPrefix(path, ex+"/") {
			return true
		}
	}
	return false
}

// diffSnapshots returns created or modified paths between two snapshots,
// sorted and collapsed to top-most entries (descendants of a changed
// ancestor are dropped — existence of the ancestor implies the subtree).
// A truncated snapshot invalidates the diff: the step degrades to
// uncacheable rather than producing a wrong output list.
func diffSnapshots(before, after fsSnapshot) []string {
	if before.truncated || after.truncated ||
		before.entries == nil || after.entries == nil {
		return nil
	}
	var changed []string
	for path, ea := range after.entries {
		eb, ok := before.entries[path]
		// A directory that merely gained or lost children (mtime/size churn)
		// is just a container — reporting it would collapse the diff to a
		// near-root anchor and hide the real outputs. Created paths of any
		// kind, plus modified files, are always reported.
		if !ok || (!ea.isDir && (ea.size != eb.size || ea.mtime != eb.mtime)) {
			changed = append(changed, path)
		}
	}
	if len(changed) == 0 {
		return nil
	}
	sort.Strings(changed)
	return collapsePaths(changed)
}

// collapsePaths drops paths already covered by a kept ancestor. Input must be
// sorted: ancestors sort before descendants, so a single-pass last-kept check
// suffices. Returns nil when the set is too noisy to be useful.
func collapsePaths(sorted []string) []string {
	out := make([]string, 0, len(sorted))
	var lastKept string
	for _, p := range sorted {
		if len(out) >= maxOutputPaths {
			return nil
		}
		if lastKept != "" && strings.HasPrefix(p, lastKept+"/") {
			continue
		}
		out = append(out, p)
		lastKept = p
	}
	return out
}
