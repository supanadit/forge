package disk

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withSnapshotRoots swaps the default watched roots for the duration of the
// test so snapshots stay hermetic (no global /usr, /home, ... traversal).
func withSnapshotRoots(t *testing.T, roots ...string) {
	t.Helper()
	old := defaultSnapshotRoots
	defaultSnapshotRoots = roots
	t.Cleanup(func() { defaultSnapshotRoots = old })
}

func TestSnapshotDiff_CreatesAndModifies(t *testing.T) {
	withSnapshotRoots(t) // no default roots: only extra roots are watched

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "existing"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "existing", "a.txt"), []byte("v1"), 0o644))

	before := takeSnapshot(root)

	// Create a nested file and modify the existing one.
	nested := filepath.Join(root, "created", "deep", "new.bin")
	require.NoError(t, os.MkdirAll(filepath.Dir(nested), 0o755))
	require.NoError(t, os.WriteFile(nested, []byte("data"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "existing", "a.txt"), []byte("v2-longer"), 0o644))

	after := takeSnapshot(root)
	diff := diffSnapshots(before, after)

	assert.Contains(t, diff, filepath.Join(root, "created"), "top-most created dir is reported")
	assert.NotContains(t, diff, nested, "descendants of a reported dir are collapsed away")
	assert.Contains(t, diff, filepath.Join(root, "existing", "a.txt"), "modified file is reported")
}

func TestDiffSnapshots_EmptyWhenUnchanged(t *testing.T) {
	withSnapshotRoots(t)
	root := t.TempDir()
	s1 := takeSnapshot(root)
	s2 := takeSnapshot(root)
	assert.Nil(t, diffSnapshots(s1, s2))
}

func TestDiffSnapshots_TruncatedBaselineYieldsNoOutputs(t *testing.T) {
	withSnapshotRoots(t)
	root := t.TempDir()
	before := takeSnapshot(root)
	before.truncated = true
	require.NoError(t, os.WriteFile(filepath.Join(root, "new.bin"), nil, 0o644))
	after := takeSnapshot(root)
	assert.Nil(t, diffSnapshots(before, after), "incomplete snapshots must degrade to uncacheable")
}

func TestCollapsePaths_AncestorCoversDescendants(t *testing.T) {
	sorted := []string{
		"/usr/local/new",
		"/usr/local/new/a",
		"/usr/local/new/a/b.txt",
		"/usr/local/other.txt",
	}
	got := collapsePaths(sorted)
	assert.Equal(t, []string{"/usr/local/new", "/usr/local/other.txt"}, got)
}

func TestCollapsePaths_CapReturnsNil(t *testing.T) {
	var paths []string
	for i := 0; i <= maxOutputPaths+1; i++ {
		paths = append(paths, filepath.Join("/x", string(rune('a'+i%26))+string(rune(i))))
	}
	assert.Nil(t, collapsePaths(paths), "over-noisy diffs must be abandoned")
}

func TestExcludedPath(t *testing.T) {
	assert.True(t, excludedPath("/proc"))
	assert.True(t, excludedPath("/proc/1/status"))
	assert.True(t, excludedPath("/var/cache/apt/archives"))
	assert.False(t, excludedPath("/var/lib/postgresql"))
	assert.False(t, excludedPath("/usr/local/bin/tool"))
}

func TestWalkRoot_ExtraRootUnderExcludedPrefixIsWalked(t *testing.T) {
	withSnapshotRoots(t)

	// Shell steps may build inside a fetched source tree under /tmp; their
	// working directory must still be captured.
	root := t.TempDir() // typically /tmp/...
	src := filepath.Join(root, "src", "proto")
	require.NoError(t, os.MkdirAll(src, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(src, "gen.pb-c.c"), []byte("//"), 0o644))

	m := fsSnapshot{entries: map[string]snapshotEntry{}}
	walkRoot(filepath.Join(root, "src"), false, &m)
	_, ok := m.entries[filepath.Join(src, "gen.pb-c.c")]
	assert.True(t, ok, "extra roots bypass exclusions")

	before := takeSnapshot(filepath.Join(root, "src"))
	require.NoError(t, os.WriteFile(filepath.Join(src, "gen2.c"), nil, 0o644))
	after := takeSnapshot(filepath.Join(root, "src"))
	diff := diffSnapshots(before, after)
	assert.True(t, slices.Contains(diff, filepath.Join(src, "gen2.c")))
}
