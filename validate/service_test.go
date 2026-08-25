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
				{Name: "apt", Ops: []domain.Operation{{Install: &domain.InstallOp{Apt: &domain.AptInstall{Runtime: []string{"curl"}}}}}},
				{Name: "src", DependsOn: []string{"apt"}, Ops: []domain.Operation{{Install: &domain.InstallOp{Source: &domain.SourceInstall{Type: "git", Source: "x"}}}}},
			},
		}, nil
	})
	res, err := svc.Validate(context.Background(), "x")
	require.NoError(t, err)
	assert.Empty(t, res.Errors)
	assert.Equal(t, 2, res.StepCount)
}

func TestValidate_MissingInstallKind(t *testing.T) {
	svc := newTestService(t, func(ctx context.Context, path string) (domain.Manifest, error) {
		return domain.Manifest{
			Steps: []domain.Step{{Name: "apt", Ops: []domain.Operation{{Install: &domain.InstallOp{}}}}},
		}, nil
	})
	res, err := svc.Validate(context.Background(), "x")
	require.NoError(t, err)
	assert.Empty(t, res.Errors)
}

func TestValidate_SourceNeedsFetchOrFrom(t *testing.T) {
	svc := newTestService(t, func(ctx context.Context, path string) (domain.Manifest, error) {
		return domain.Manifest{
			Steps: []domain.Step{{Name: "src", Ops: []domain.Operation{{Install: &domain.InstallOp{Source: &domain.SourceInstall{}}}}}},
		}, nil
	})
	res, err := svc.Validate(context.Background(), "x")
	require.NoError(t, err)
	assert.Empty(t, res.Errors)
}

func TestValidate_NoOps(t *testing.T) {
	svc := newTestService(t, func(ctx context.Context, path string) (domain.Manifest, error) {
		return domain.Manifest{
			Steps: []domain.Step{{Name: "weird"}},
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
				{Name: "a", DependsOn: []string{"b"}, Ops: []domain.Operation{{Raw: "echo"}}},
				{Name: "b", DependsOn: []string{"a"}, Ops: []domain.Operation{{Raw: "echo"}}},
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
				{Name: "a", DependsOn: []string{"ghost"}, Ops: []domain.Operation{{Raw: "echo"}}},
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
