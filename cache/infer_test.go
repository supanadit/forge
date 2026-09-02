package cache

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/supanadit/forge/domain"
)

// fileCreatingInner is a StepExecutor whose shell steps "produce" a real file
// and report it as an auto-discovered output, mimicking disk.Executor's
// snapshot diff behaviour.
type fileCreatingInner struct {
	calls   int
	targets map[string]string // step name -> created file path
}

func (f *fileCreatingInner) Execute(_ context.Context, step domain.Step, _ domain.StepContext) (domain.StepResult, error) {
	f.calls++
	res := domain.StepResult{Name: step.Name, Status: domain.StepStatusSuccess}
	if target, ok := f.targets[step.Name]; ok {
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			panic(err)
		}
		if err := os.WriteFile(target, []byte("data"), 0o644); err != nil {
			panic(err)
		}
		res.Outputs = []string{target}
	}
	return res, nil
}

func TestVerify_ShellRecordedOutputs(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "out.bin")
	require.NoError(t, os.WriteFile(existing, []byte("x"), 0o644))

	step := domain.Step{Ops: []domain.Operation{{Raw: "echo"}}}
	assert.True(t, Verify(step, CacheFile{Outputs: []string{existing}}, nil))

	missing := filepath.Join(dir, "gone.bin")
	assert.False(t, Verify(step, CacheFile{Outputs: []string{existing, missing}}, nil))

	// Entry without outputs is unverifiable.
	assert.False(t, Verify(step, CacheFile{}, nil))
}

func TestVerify_SourceInferenceFallbacks(t *testing.T) {
	prefix := t.TempDir()
	step := domain.Step{
		Ops: []domain.Operation{{SourceInstall: &domain.SourceInstall{Prefix: prefix}}},
	}
	assert.False(t, Verify(step, CacheFile{}, nil), "an empty prefix dir does not count as output")
	require.NoError(t, os.MkdirAll(filepath.Join(prefix, "bin"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(prefix, "bin", "psql"), nil, 0o644))
	assert.True(t, Verify(step, CacheFile{}, nil))
	require.NoError(t, os.RemoveAll(prefix))
	assert.False(t, Verify(step, CacheFile{}, nil))

	verifyFile := filepath.Join(t.TempDir(), "control")
	require.NoError(t, os.WriteFile(verifyFile, nil, 0o644))
	withVerify := domain.Step{
		Ops: []domain.Operation{{SourceInstall: &domain.SourceInstall{Verify: []domain.VerifyCheck{{File: verifyFile}}}}},
	}
	assert.True(t, Verify(withVerify, CacheFile{}, nil))
	assert.False(t, Verify(withVerify, CacheFile{Outputs: []string{"/nonexistent"}}, nil), "recorded outputs take precedence")
}

type fakeChecker struct{ installed map[string]bool }

func (f *fakeChecker) Installed(pkg string) bool { return f.installed[pkg] }

func TestVerify_PackagesUsesChecker(t *testing.T) {
	checker := &fakeChecker{installed: map[string]bool{"curl": true}}
	step := domain.Step{
		Ops: []domain.Operation{{Packages: &domain.PackagesOp{Build: []string{"build-essential"}, Runtime: []string{"curl"}}}},
	}
	assert.False(t, Verify(step, CacheFile{}, nil), "nil checker cannot verify packages")
	assert.False(t, Verify(step, CacheFile{}, checker), "build-essential not installed")

	checker.installed["build-essential"] = true
	assert.True(t, Verify(step, CacheFile{}, checker))
}

func TestOutputPaths_Inference(t *testing.T) {
	src := domain.Step{
		Ops: []domain.Operation{{SourceInstall: &domain.SourceInstall{
			Prefix: "/usr/local/pgsql",
			Verify: []domain.VerifyCheck{{File: "/usr/local/pgsql/share/extension/citus.control"}},
		}}},
	}
	assert.Equal(t,
		[]string{"/usr/local/pgsql/share/extension/citus.control", "/usr/local/pgsql"},
		OutputPaths(src))

	bin := domain.Step{
		Ops: []domain.Operation{{BinaryInstall: &domain.BinaryInstall{URL: "https://x.tgz", Copy: []domain.CopySpec{{From: "m", To: "/usr/local/bin/pgmetrics"}}}}},
	}
	assert.Equal(t, []string{"/usr/local/bin/pgmetrics"}, OutputPaths(bin))

	pkgs := domain.Step{
		Ops: []domain.Operation{{Packages: &domain.PackagesOp{Runtime: []string{"curl"}}}},
	}
	assert.Nil(t, OutputPaths(pkgs), "package installs have no static output paths")

	assert.Nil(t, OutputPaths(domain.Step{Ops: []domain.Operation{{Raw: "echo"}}}),
		"raw-shell outputs come from snapshot diffs, not static paths")
}

func TestCachedExecutor_RestoresOutputsFromArtifact(t *testing.T) {
	work := t.TempDir()
	svc, err := New(t.TempDir())
	require.NoError(t, err)

	target := filepath.Join(work, "installed", "tool")
	inner := &fileCreatingInner{targets: map[string]string{"mk-tool": target}}
	ce := NewCachedExecutor(inner, svc)
	step := domain.Step{Name: "mk-tool", Ops: []domain.Operation{{Raw: "make install"}}}
	sctx := domain.StepContext{Vars: map[string]string{}, Previous: map[string]domain.StepResult{}, Project: "proj"}

	// First run executes and persists metadata + artifact.
	res1, err := ce.Execute(context.Background(), step, sctx)
	require.NoError(t, err)
	assert.Equal(t, domain.StepStatusSuccess, res1.Status)
	assert.Equal(t, 1, inner.calls)

	// Simulate a Docker-style filesystem reset.
	require.NoError(t, os.RemoveAll(filepath.Join(work, "installed")))

	// Second run restores from the artifact archive instead of re-executing.
	res2, err := ce.Execute(context.Background(), step, sctx)
	require.NoError(t, err)
	assert.Equal(t, domain.StepStatusCached, res2.Status)
	assert.Equal(t, 1, inner.calls, "artifact restore must skip re-execution")

	data, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "data", string(data))
}

func TestPrune_RemovesArtifacts(t *testing.T) {
	svc, err := New(t.TempDir())
	require.NoError(t, err)

	// Metadata + artifacts for two steps.
	require.NoError(t, svc.Save("proj", "keep", "k1", CacheFile{}))
	require.NoError(t, svc.SaveArtifact("proj", "keep", "k1", []string{"/etc/hosts"}))
	require.NoError(t, svc.Save("proj", "gone", "k2", CacheFile{}))
	require.NoError(t, svc.SaveArtifact("proj", "gone", "k2", []string{"/etc/hosts"}))

	require.NoError(t, svc.Prune("proj", map[string]bool{"keep": true}))

	_, ok := svc.Lookup("proj", "keep", "k1")
	assert.True(t, ok)
	_, err = os.Stat(svc.ArtifactPath("proj", "keep", "k1"))
	assert.NoError(t, err, "kept step's artifact must survive pruning")
	_, err = os.Stat(svc.ArtifactPath("proj", "gone", "k2"))
	assert.True(t, os.IsNotExist(err), "pruned step's artifact must be removed")

	// Full clean removes everything including artifacts.
	require.NoError(t, svc.Prune("proj", nil))
	_, err = os.Stat(svc.ArtifactPath("proj", "keep", "k1"))
	assert.True(t, os.IsNotExist(err))
}

func TestSaveArtifact_SkipsEmptyPathList(t *testing.T) {
	svc, err := New(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, svc.SaveArtifact("proj", "noop", "k", nil))
	_, err = os.Stat(svc.ArtifactPath("proj", "noop", "k"))
	assert.True(t, os.IsNotExist(err))
}
