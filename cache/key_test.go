package cache

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/supanadit/forge/domain"
)

func TestComputeKey_StableForSameInputs(t *testing.T) {
	step := domain.Step{
		Name: "build-pg",
		Ops:  []domain.Operation{{SourceInstall: &domain.SourceInstall{Type: "git", URL: "https://x/repo.git"}}},
	}
	vars := map[string]string{"POSTGRESQL_VERSION": "13.5"}
	deps := map[string]string{"deps": "abc"}

	k1 := ComputeKey(step, vars, deps)
	k2 := ComputeKey(step, vars, deps)
	assert.Equal(t, k1, k2)
}

func TestComputeKey_ChangesWithConfig(t *testing.T) {
	base := domain.Step{
		Name: "build-pg",
		Ops:  []domain.Operation{{SourceInstall: &domain.SourceInstall{Type: "git", URL: "https://x/repo.git"}}},
	}
	vars := map[string]string{}
	deps := map[string]string{}

	k1 := ComputeKey(base, vars, deps)
	base.Ops[0].SourceInstall.URL = "https://x/other.git"
	k2 := ComputeKey(base, vars, deps)
	require.NotEqual(t, k1, k2, "changing the config must change the key")
}

func TestComputeKey_ChangesWithVars(t *testing.T) {
	step := domain.Step{
		Name: "build-pg",
		Ops:  []domain.Operation{{SourceInstall: &domain.SourceInstall{Type: "git", URL: "https://x/repo.git", Ref: "${PG_VERSION}"}}},
	}
	deps := map[string]string{}
	k1 := ComputeKey(step, map[string]string{"PG_VERSION": "13"}, deps)
	k2 := ComputeKey(step, map[string]string{"PG_VERSION": "14"}, deps)
	require.NotEqual(t, k1, k2, "changing a referenced var must change the key")
}

func TestComputeKey_DepChange(t *testing.T) {
	step := domain.Step{
		Name:      "build-pg",
		DependsOn: []string{"install-system-deps"},
		Ops:       []domain.Operation{{SourceInstall: &domain.SourceInstall{Type: "git", URL: "https://x/repo.git"}}},
	}
	vars := map[string]string{}
	k1 := ComputeKey(step, vars, map[string]string{"base": "keyA"})
	k2 := ComputeKey(step, vars, map[string]string{"base": "keyB"})
	require.NotEqual(t, k1, k2, "changing a dependency key must change the step key")
}

func TestComputeKey_PackagesIncluded(t *testing.T) {
	step := domain.Step{
		Name: "deps",
		Ops:  []domain.Operation{{Packages: &domain.PackagesOp{Build: []string{"build-essential"}, Runtime: []string{"curl"}, Remove: []string{"vim-tiny"}}}},
	}
	k1 := ComputeKey(step, map[string]string{}, map[string]string{})
	step.Ops[0].Packages.Remove = []string{"nano"}
	k2 := ComputeKey(step, map[string]string{}, map[string]string{})
	require.NotEqual(t, k1, k2, "changing the remove list must change the key")
}