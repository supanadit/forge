package disk

import (
	"context"
	"os"
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
		Name:  "echo",
		Kind:  domain.StepKindShell,
		Shell: &domain.ShellStep{Commands: []string{"echo hello"}},
	}
	res, err := e.Execute(context.Background(), step, domain.StepContext{})
	require.NoError(t, err)
	assert.Equal(t, domain.StepStatusSuccess, res.Status)
}

func TestExecute_ShellStepFailure(t *testing.T) {
	e := newTestExecutor(t)
	step := domain.Step{
		Name:  "fail",
		Kind:  domain.StepKindShell,
		Shell: &domain.ShellStep{Commands: []string{"exit 1"}},
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
		Kind: domain.StepKindShell,
		Shell: &domain.ShellStep{
			Commands: []string{"echo ${MSG} > " + filepath.Join(dir, "out.txt")},
		},
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
		Name:   "check",
		Kind:   domain.StepKindVerify,
		Verify: &domain.VerifyStep{Checks: []domain.VerifyCheck{{File: existing}}},
	}
	res, err := e.Execute(context.Background(), step, domain.StepContext{})
	require.NoError(t, err)
	assert.Equal(t, domain.StepStatusSuccess, res.Status)
}

func TestExecute_VerifyFailsOnMissingFile(t *testing.T) {
	e := newTestExecutor(t)
	step := domain.Step{
		Name:   "check",
		Kind:   domain.StepKindVerify,
		Verify: &domain.VerifyStep{Checks: []domain.VerifyCheck{{File: "/nonexistent/xyz"}}},
	}
	res, err := e.Execute(context.Background(), step, domain.StepContext{})
	require.Error(t, err)
	assert.Equal(t, domain.StepStatusFailed, res.Status)
}

func TestExecute_UnknownKind(t *testing.T) {
	e := newTestExecutor(t)
	res, err := e.Execute(context.Background(), domain.Step{Name: "x", Kind: "weird"}, domain.StepContext{})
	require.Error(t, err)
	assert.Equal(t, domain.StepStatusFailed, res.Status)
}

func TestExecute_SourceFromPriorStep(t *testing.T) {
	e := newTestExecutor(t)
	srcDir := t.TempDir()
	// A trivial Makefile so the make strategy succeeds.
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "Makefile"), []byte("all:\n\t@echo built\ninstall:\n\t@echo installed\n"), 0o644))
	prev := map[string]domain.StepResult{
		"fetch-src": {Name: "fetch-src", Status: domain.StepStatusSuccess, SourceDir: srcDir},
	}
	step := domain.Step{
		Name: "build",
		Kind: domain.StepKindSource,
		Source: &domain.SourceStep{
			From:    "fetch-src",
			Build:   &domain.BuildSpec{Strategy: domain.BuildStrategyMake},
			Install: false,
		},
	}
	res, err := e.Execute(context.Background(), step, domain.StepContext{Previous: prev})
	require.NoError(t, err)
	assert.Equal(t, srcDir, res.SourceDir)
}

func TestExecute_AptUnknownAction(t *testing.T) {
	e := newTestExecutor(t)
	step := domain.Step{
		Name: "apt",
		Kind: domain.StepKindApt,
		Apt:  &domain.AptStep{Action: "explode", Packages: []string{"x"}},
	}
	res, err := e.Execute(context.Background(), step, domain.StepContext{})
	require.Error(t, err)
	assert.Equal(t, domain.StepStatusFailed, res.Status)
}

func TestExecute_AptConditionalUsesVars(t *testing.T) {
	e := newTestExecutor(t)
	step := domain.Step{
		Name: "deps",
		Kind: domain.StepKindApt,
		Apt: &domain.AptStep{
			Action: "install",
			PackagesConditional: []domain.ConditionalPackages{
				{Condition: "${POSTGRESQL_VERSION%%.*} -ge 17", Packages: []string{"bison"}},
			},
		},
	}
	// POSTGRESQL_VERSION=13.5 → 13 < 17 → bison must NOT be installed.
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
		Kind: domain.StepKindShell,
		Shell: &domain.ShellStep{
			Commands: []string{"echo ${step:build-pg.source} > " + filepath.Join(srcDir, "src.txt"), "echo ${step:build-pg.prefix} > " + filepath.Join(srcDir, "pfx.txt")},
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

func TestEvaluateCondition(t *testing.T) {
	ok, err := evalCondition(context.Background(), "1 -eq 1", nil)
	require.NoError(t, err)
	assert.True(t, ok)

	ok, err = evalCondition(context.Background(), "1 -eq 2", nil)
	require.NoError(t, err)
	assert.False(t, ok)

	// ${VAR%%.*} bash expansion resolves from the environment.
	ok, err = evalCondition(context.Background(), `${PG%%.*} -eq 13`, []string{"PG=13.5"})
	require.NoError(t, err)
	assert.True(t, ok)
}
