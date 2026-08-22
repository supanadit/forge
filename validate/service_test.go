package validate

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/supanadit/forge/domain"
	"github.com/supanadit/forge/manifest"
	"github.com/supanadit/forge/scheduler"
)

type repoFunc func(ctx context.Context, path string) (domain.Manifest, error)

type fakeRepo struct{ fn repoFunc }

func (r *fakeRepo) Load(ctx context.Context, path string) (domain.Manifest, error) {
	return r.fn(ctx, path)
}

func newTestService(t *testing.T, fn repoFunc) *Service {
	t.Helper()
	return NewService(manifest.NewService(&fakeRepo{fn: fn}), scheduler.NewService())
}

func TestValidate_ValidManifest(t *testing.T) {
	svc := newTestService(t, func(ctx context.Context, path string) (domain.Manifest, error) {
		return domain.Manifest{
			Steps: []domain.Step{
				{Name: "apt", Kind: domain.StepKindApt, Apt: &domain.AptStep{Action: "install", Packages: []string{"curl"}}},
				{Name: "src", DependsOn: []string{"apt"}, Kind: domain.StepKindSource,
					Source: &domain.SourceStep{Fetch: &domain.FetchSpec{Type: domain.FetchTypeGit, Git: &domain.GitFetch{URL: "x"}}, Install: true}},
			},
		}, nil
	})
	res, err := svc.Validate(context.Background(), "x")
	require.NoError(t, err)
	assert.Empty(t, res.Errors)
	assert.Equal(t, 2, res.StepCount)
}

func TestValidate_MissingAptAction(t *testing.T) {
	svc := newTestService(t, func(ctx context.Context, path string) (domain.Manifest, error) {
		return domain.Manifest{
			Steps: []domain.Step{{Name: "apt", Kind: domain.StepKindApt, Apt: &domain.AptStep{}}},
		}, nil
	})
	res, err := svc.Validate(context.Background(), "x")
	require.NoError(t, err)
	assert.Len(t, res.Errors, 1)
}

func TestValidate_SourceNeedsFetchOrFrom(t *testing.T) {
	svc := newTestService(t, func(ctx context.Context, path string) (domain.Manifest, error) {
		return domain.Manifest{
			Steps: []domain.Step{{Name: "src", Kind: domain.StepKindSource, Source: &domain.SourceStep{}}},
		}, nil
	})
	res, err := svc.Validate(context.Background(), "x")
	require.NoError(t, err)
	assert.Len(t, res.Errors, 1)
}

func TestValidate_UnknownKind(t *testing.T) {
	svc := newTestService(t, func(ctx context.Context, path string) (domain.Manifest, error) {
		return domain.Manifest{
			Steps: []domain.Step{{Name: "weird", Kind: "frobnicate"}},
		}, nil
	})
	res, err := svc.Validate(context.Background(), "x")
	require.NoError(t, err)
	assert.Len(t, res.Errors, 1)
}

func TestValidate_CircularDependencyReported(t *testing.T) {
	svc := newTestService(t, func(ctx context.Context, path string) (domain.Manifest, error) {
		return domain.Manifest{
			Steps: []domain.Step{
				{Name: "a", DependsOn: []string{"b"}, Kind: domain.StepKindShell, Shell: &domain.ShellStep{Commands: []string{"echo"}}},
				{Name: "b", DependsOn: []string{"a"}, Kind: domain.StepKindShell, Shell: &domain.ShellStep{Commands: []string{"echo"}}},
			},
		}, nil
	})
	res, err := svc.Validate(context.Background(), "x")
	require.NoError(t, err)
	assert.Contains(t, res.Errors[0], "circular")
}

func TestValidate_UnknownDependencyReported(t *testing.T) {
	svc := newTestService(t, func(ctx context.Context, path string) (domain.Manifest, error) {
		return domain.Manifest{
			Steps: []domain.Step{
				{Name: "a", DependsOn: []string{"ghost"}, Kind: domain.StepKindShell, Shell: &domain.ShellStep{Commands: []string{"echo"}}},
			},
		}, nil
	})
	res, err := svc.Validate(context.Background(), "x")
	require.NoError(t, err)
	assert.Contains(t, res.Errors[0], "unknown")
}

func TestValidate_ManifestLoadError(t *testing.T) {
	svc := newTestService(t, func(ctx context.Context, path string) (domain.Manifest, error) {
		return domain.Manifest{}, domain.ErrManifestNotFound
	})
	_, err := svc.Validate(context.Background(), "missing")
	assert.ErrorIs(t, err, domain.ErrManifestNotFound)
}
