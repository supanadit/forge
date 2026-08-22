package cache

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/supanadit/forge/domain"
)

func TestComputeKey_StableForSameInputs(t *testing.T) {
	step := domain.Step{
		Name:   "build-pg",
		Kind:   domain.StepKindSource,
		Source: &domain.SourceStep{Fetch: &domain.FetchSpec{Type: domain.FetchTypeGit}},
	}
	vars := map[string]string{"POSTGRESQL_VERSION": "13.5"}
	deps := map[string]string{"deps": "abc"}

	k1 := ComputeKey(step, vars, deps)
	k2 := ComputeKey(step, vars, deps)
	assert.Equal(t, k1, k2)
}

func TestComputeKey_ChangesWithConfig(t *testing.T) {
	base := domain.Step{
		Name:   "build-pg",
		Kind:   domain.StepKindSource,
		Source: &domain.SourceStep{Fetch: &domain.FetchSpec{Type: domain.FetchTypeGit}},
	}
	vars := map[string]string{}
	deps := map[string]string{}

	k1 := ComputeKey(base, vars, deps)
	base.Source.Fetch.Git = &domain.GitFetch{URL: "https://x/repo.git"}
	k2 := ComputeKey(base, vars, deps)
	require.NotEqual(t, k1, k2, "changing the config must change the key")
}

func TestComputeKey_ChangesWithVars(t *testing.T) {
	step := domain.Step{
		Name:   "build-pg",
		Kind:   domain.StepKindSource,
		Source: &domain.SourceStep{Fetch: &domain.FetchSpec{Type: domain.FetchTypeGit, Git: &domain.GitFetch{URL: "https://x/repo.git", Ref: "${PG_VERSION}"}}},
	}
	deps := map[string]string{}
	k1 := ComputeKey(step, map[string]string{"PG_VERSION": "13"}, deps)
	k2 := ComputeKey(step, map[string]string{"PG_VERSION": "14"}, deps)
	require.NotEqual(t, k1, k2, "changing a referenced var must change the key")
}

func TestComputeKey_DepChange(t *testing.T) {
	step := domain.Step{
		Name:      "build-pg",
		Kind:      domain.StepKindSource,
		DependsOn: []string{"install-system-deps"},
		Source:    &domain.SourceStep{Fetch: &domain.FetchSpec{Type: domain.FetchTypeGit}},
	}
	vars := map[string]string{}
	k1 := ComputeKey(step, vars, map[string]string{"base": "keyA"})
	k2 := ComputeKey(step, vars, map[string]string{"base": "keyB"})
	require.NotEqual(t, k1, k2, "changing a dependency key must change the step key")
}
