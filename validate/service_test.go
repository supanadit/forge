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
				{Name: "deps", Ops: []domain.Operation{{Packages: &domain.PackagesOp{Runtime: []string{"curl"}}}}},
				{Name: "src", DependsOn: []string{"deps"}, Ops: []domain.Operation{{SourceInstall: &domain.SourceInstall{Type: "git", URL: "x"}}}},
			},
		}, nil
	})
	res, err := svc.Validate(context.Background(), "x")
	require.NoError(t, err)
	assert.Empty(t, res.Errors)
	assert.Equal(t, 2, res.StepCount)
}

func TestValidate_EmptyPackagesOp(t *testing.T) {
	svc := newTestService(t, func(ctx context.Context, path string) (domain.Manifest, error) {
		return domain.Manifest{
			Steps: []domain.Step{{Name: "deps", Ops: []domain.Operation{{Packages: &domain.PackagesOp{}}}}},
		}, nil
	})
	res, err := svc.Validate(context.Background(), "x")
	require.NoError(t, err)
	assert.Len(t, res.Errors, 1)
	assert.Contains(t, res.Errors[0], "packages")
}

func TestValidate_SourceNeedsFetchOrFrom(t *testing.T) {
	svc := newTestService(t, func(ctx context.Context, path string) (domain.Manifest, error) {
		return domain.Manifest{
			Steps: []domain.Step{{Name: "src", Ops: []domain.Operation{{SourceInstall: &domain.SourceInstall{}}}}},
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

func TestValidate_SourceInstallChecksum(t *testing.T) {
	const hex64 = "a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2"
	valid := func(cs, ct string) func(context.Context, string) (domain.Manifest, error) {
		return func(ctx context.Context, path string) (domain.Manifest, error) {
			return domain.Manifest{Steps: []domain.Step{
				{Name: "src", Ops: []domain.Operation{{SourceInstall: &domain.SourceInstall{
					Type: "archive", URL: "x", Checksum: cs, ChecksumType: ct,
				}}}},
			}}, nil
		}
	}

	// Valid: sha256 digest with explicit and inferred type, and a checksum-file line.
	for _, tc := range []struct{ cs, ct string }{
		{hex64, "sha256"},
		{hex64, ""},
		{hex64 + "  file.tar.gz", ""},
		{"sha256:" + hex64, ""},
	} {
		res, err := newTestService(t, valid(tc.cs, tc.ct)).Validate(context.Background(), "x")
		require.NoError(t, err)
		assert.Empty(t, res.Errors, "checksum %q type %q should be valid", tc.cs, tc.ct)
	}

	// Invalid: wrong length, non-hex, and unknown type.
	for _, tc := range []struct{ cs, ct string }{
		{"deadbeef", ""},
		{"zzzz", "sha256"},
		{hex64, "sha512"},
		{hex64[:60], "sha256"},
	} {
		res, err := newTestService(t, valid(tc.cs, tc.ct)).Validate(context.Background(), "x")
		require.NoError(t, err)
		assert.NotEmpty(t, res.Errors, "checksum %q type %q should be invalid", tc.cs, tc.ct)
	}
}
