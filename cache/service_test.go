package cache

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/supanadit/forge/domain"
)

func TestService_SaveLookup(t *testing.T) {
	svc, err := New(t.TempDir())
	require.NoError(t, err)
	cf := CacheFile{SourceDir: "/src", Prefix: "/usr/local/pgsql", Deps: map[string]string{"d": "k"}}
	require.NoError(t, svc.Save("pg", "install-pg", "key1", cf))

	got, ok := svc.Lookup("pg", "install-pg", "key1")
	require.True(t, ok)
	assert.Equal(t, "install-pg", got.Name)
	assert.Equal(t, "/usr/local/pgsql", got.Prefix)

	// Wrong key → miss.
	_, ok = svc.Lookup("pg", "install-pg", "otherkey")
	assert.False(t, ok)

	// Wrong project → miss.
	_, ok = svc.Lookup("other", "install-pg", "key1")
	assert.False(t, ok)
}

func TestService_Prune(t *testing.T) {
	dir := t.TempDir()
	svc, err := New(dir)
	require.NoError(t, err)
	require.NoError(t, svc.Save("proj", "keep", "k1", CacheFile{}))
	require.NoError(t, svc.Save("proj", "remove", "k2", CacheFile{}))

	require.NoError(t, svc.Prune("proj", map[string]bool{"keep": true}))

	_, ok := svc.Lookup("proj", "keep", "k1")
	assert.True(t, ok, "keep should survive")
	_, ok = svc.Lookup("proj", "remove", "k2")
	assert.False(t, ok, "remove should be pruned")
}

func TestService_List(t *testing.T) {
	svc, err := New(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, svc.Save("proj", "a", "k1", CacheFile{}))
	require.NoError(t, svc.Save("proj", "b", "k2", CacheFile{}))
	entries, err := svc.List("proj")
	require.NoError(t, err)
	assert.Len(t, entries, 2)
}

func TestCacheable(t *testing.T) {
	assert.False(t, Cacheable(domain.Step{Kind: domain.StepKindVerify}))
	assert.False(t, Cacheable(domain.Step{Kind: domain.StepKindShell, Shell: &domain.ShellStep{}}))
	assert.True(t, Cacheable(domain.Step{Kind: domain.StepKindShell, Shell: &domain.ShellStep{CacheVerify: []domain.VerifyCheck{{File: "/x"}}}}))
	assert.True(t, Cacheable(domain.Step{Kind: domain.StepKindApt, Apt: &domain.AptStep{Action: "install", Packages: []string{"curl"}}}))
	assert.False(t, Cacheable(domain.Step{Kind: domain.StepKindApt, Apt: &domain.AptStep{Action: "remove"}}))
	assert.True(t, Cacheable(domain.Step{Kind: domain.StepKindSource, Source: &domain.SourceStep{Verify: []domain.VerifyCheck{{File: "/x"}}}}))
}

// fakeInner is a StepExecutor that records how many times it was called.
type fakeInner struct {
	calls int
}

func (f *fakeInner) Execute(_ context.Context, step domain.Step, _ domain.StepContext) (domain.StepResult, error) {
	f.calls++
	return domain.StepResult{Name: step.Name, Status: domain.StepStatusSuccess}, nil
}

func TestCachedExecutor_HitSkipsInner(t *testing.T) {
	dir := t.TempDir()
	svc, err := New(dir)
	require.NoError(t, err)
	inner := &fakeInner{}
	ce := NewCachedExecutor(inner, svc)

	step := domain.Step{
		Name:  "link",
		Kind:  domain.StepKindShell,
		Shell: &domain.ShellStep{CacheVerify: []domain.VerifyCheck{{File: "/usr/bin/tool"}}},
	}
	sctx := domain.StepContext{Vars: map[string]string{}, Previous: map[string]domain.StepResult{}, Project: "proj"}

	// First run: no cache file, executes inner.
	res1, err := ce.Execute(context.Background(), step, sctx)
	require.NoError(t, err)
	assert.Equal(t, domain.StepStatusSuccess, res1.Status)
	assert.Equal(t, 1, inner.calls)

	// Second run: cache saved, but verify fails (file doesn't exist) → executes again.
	res2, err := ce.Execute(context.Background(), step, sctx)
	require.NoError(t, err)
	assert.Equal(t, domain.StepStatusSuccess, res2.Status)
	assert.Equal(t, 2, inner.calls)
}

func TestCachedExecutor_NoCache(t *testing.T) {
	dir := t.TempDir()
	svc, err := New(dir)
	require.NoError(t, err)
	inner := &fakeInner{}
	ce := NewCachedExecutor(inner, svc)
	step := domain.Step{Name: "x", Kind: domain.StepKindVerify}
	sctx := domain.StepContext{NoCache: true, Project: "proj"}
	_, err = ce.Execute(context.Background(), step, sctx)
	require.NoError(t, err)
	assert.Equal(t, 1, inner.calls)
}
