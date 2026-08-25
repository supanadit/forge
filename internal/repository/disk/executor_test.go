package disk

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/supanadit/forge/builder"
	"github.com/supanadit/forge/domain"
	"github.com/supanadit/forge/fetch"
)

func newTestExecutor(t *testing.T) *Executor {
	t.Helper()
	f := fetch.NewService(NewFetchRepository())
	b := builder.NewService(NewBuildRepository())
	return NewExecutor(f, b)
}

func TestExecute_ShellStep(t *testing.T) {
	e := newTestExecutor(t)
	step := domain.Step{
		Name: "echo",
		Ops:  []domain.Operation{{Raw: "echo hello"}},
	}
	res, err := e.Execute(context.Background(), step, domain.StepContext{})
	require.NoError(t, err)
	assert.Equal(t, domain.StepStatusSuccess, res.Status)
}

func TestExecute_ShellStepFailure(t *testing.T) {
	e := newTestExecutor(t)
	step := domain.Step{
		Name: "fail",
		Ops:  []domain.Operation{{Raw: "exit 1"}},
	}
	res, err := e.Execute(context.Background(), step, domain.StepContext{})
	require.Error(t, err)
	assert.Equal(t, domain.StepStatusFailed, res.Status)
}

func TestExecute_ShellInterpolatesVars(t *testing.T) {
	e := newTestExecutor(t)
	// Write a marker to prove interpolation happened.
	dir := t.TempDir()
	step := domain.Step{
		Name: "interp",
		Ops:  []domain.Operation{{Raw: "echo ${MSG} > " + filepath.Join(dir, "out.txt")}},
	}
	res, err := e.Execute(context.Background(), step, domain.StepContext{Vars: map[string]string{"MSG": "interpolated"}})
	require.NoError(t, err)
	assert.Equal(t, domain.StepStatusSuccess, res.Status)
	data, err := os.ReadFile(filepath.Join(dir, "out.txt"))
	require.NoError(t, err)
	assert.Equal(t, "interpolated\n", string(data))
}

func TestExecute_VerifyStep(t *testing.T) {
	e := newTestExecutor(t)
	existing := filepath.Join(t.TempDir(), "exists")
	require.NoError(t, os.WriteFile(existing, []byte("x"), 0o644))

	step := domain.Step{
		Name: "check",
		Ops:  []domain.Operation{{Verify: []domain.VerifyCheck{{File: existing}}}},
	}
	res, err := e.Execute(context.Background(), step, domain.StepContext{})
	require.NoError(t, err)
	assert.Equal(t, domain.StepStatusSuccess, res.Status)
}

func TestExecute_VerifyFailsOnMissingFile(t *testing.T) {
	e := newTestExecutor(t)
	step := domain.Step{
		Name: "check",
		Ops:  []domain.Operation{{Verify: []domain.VerifyCheck{{File: "/nonexistent/xyz"}}}},
	}
	res, err := e.Execute(context.Background(), step, domain.StepContext{})
	require.Error(t, err)
	assert.Equal(t, domain.StepStatusFailed, res.Status)
}

func TestExecute_NoOps(t *testing.T) {
	e := newTestExecutor(t)
	res, err := e.Execute(context.Background(), domain.Step{Name: "x"}, domain.StepContext{})
	require.NoError(t, err)
	assert.Equal(t, domain.StepStatusSuccess, res.Status)
}

func TestExecute_InstallStep(t *testing.T) {
	e := newTestExecutor(t)
	// A local git repo with a trivial Makefile so the make strategy succeeds.
	repo := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(repo, "Makefile"), []byte("all:\n\t@echo built\ninstall:\n\t@echo installed\n"), 0o644))
	runGit := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		require.NoError(t, cmd.Run())
	}
	runGit("init", "-q")
	runGit("add", "Makefile")
	runGit("-c", "user.email=test@test", "-c", "user.name=test", "commit", "-q", "-m", "init")

	step := domain.Step{
		Name: "build",
		Ops: []domain.Operation{{Install: &domain.InstallOp{
			Source: &domain.SourceInstall{
				Type:     "git",
				Source:   repo,
				Strategy: "make",
			},
		}}},
	}
	res, err := e.Execute(context.Background(), step, domain.StepContext{})
	require.NoError(t, err)
	assert.Equal(t, domain.StepStatusSuccess, res.Status)
}

func TestExecute_AptInstallNoPackages(t *testing.T) {
	e := newTestExecutor(t)
	step := domain.Step{
		Name: "deps",
		Ops: []domain.Operation{{Install: &domain.InstallOp{
			Apt: &domain.AptInstall{
				Conditional: []domain.ConditionalApt{
					{Category: "runtime", When: domain.VersionCondition{Var: "POSTGRESQL_VERSION", Gte: "17"}, Packages: []string{"bison"}},
				},
			},
		}}},
	}
	// POSTGRESQL_VERSION=13.5 → 13.5 < 17 → no packages added → nothing installed.
	res, err := e.Execute(context.Background(), step, domain.StepContext{
		Vars: map[string]string{"POSTGRESQL_VERSION": "13.5"},
	})
	require.NoError(t, err)
	assert.Equal(t, domain.StepStatusSuccess, res.Status)
}

func TestStepFieldInterpolation(t *testing.T) {
	e := newTestExecutor(t)
	srcDir := t.TempDir()
	prefix := "/usr/local/pgsql"
	prev := map[string]domain.StepResult{
		"build-pg": {Name: "build-pg", Status: domain.StepStatusSuccess, SourceDir: srcDir, Prefix: prefix},
	}
	step := domain.Step{
		Name: "shell",
		Ops: []domain.Operation{
			{Raw: "echo ${step:build-pg.source} > " + filepath.Join(srcDir, "src.txt")},
			{Raw: "echo ${step:build-pg.prefix} > " + filepath.Join(srcDir, "pfx.txt")},
		},
	}
	res, err := e.Execute(context.Background(), step, domain.StepContext{Previous: prev})
	require.NoError(t, err)
	assert.Equal(t, domain.StepStatusSuccess, res.Status)

	data, err := os.ReadFile(filepath.Join(srcDir, "src.txt"))
	require.NoError(t, err)
	assert.Equal(t, srcDir+"\n", string(data))
	data, err = os.ReadFile(filepath.Join(srcDir, "pfx.txt"))
	require.NoError(t, err)
	assert.Equal(t, prefix+"\n", string(data))
}

func TestVersionCompare(t *testing.T) {
	assert.True(t, versionCompare("13.5", "17") < 0)
	assert.True(t, versionCompare("17", "13.5") > 0)
	assert.Equal(t, 0, versionCompare("13.5", "13.5"))
	assert.True(t, versionCompare("v3.4.1", "3.4.0") > 0)
	assert.Equal(t, 0, versionCompare("13.5", "13.5.0"))
}

func TestEvalVersionCondition(t *testing.T) {
	assert.True(t, evalVersionCondition(domain.VersionCondition{Gte: "17"}, "18"))
	assert.False(t, evalVersionCondition(domain.VersionCondition{Gte: "17"}, "13.5"))
	assert.True(t, evalVersionCondition(domain.VersionCondition{Lt: "17"}, "13.5"))
	assert.False(t, evalVersionCondition(domain.VersionCondition{Eq: "17"}, "13.5"))
	assert.False(t, evalVersionCondition(domain.VersionCondition{}, "13.5"))
}
