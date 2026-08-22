package disk

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/supanadit/forge/builder"
	"github.com/supanadit/forge/domain"
	"github.com/supanadit/forge/fetch"
)

// makeTarGz builds a tar.gz with the given files in memory at path.
func makeTarGz(t *testing.T, path string, files map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	require.NoError(t, err)
	defer f.Close()
	gz := gzip.NewWriter(f)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()
	for name, content := range files {
		body := []byte(content)
		hdr := &tar.Header{
			Name: name,
			Mode: 0o644,
			Size: int64(len(body)),
		}
		require.NoError(t, tw.WriteHeader(hdr))
		_, err := tw.Write(body)
		require.NoError(t, err)
	}
}

func TestFetchArchive_DownloadAndExtract(t *testing.T) {
	cache := t.TempDir()
	tarPath := filepath.Join(t.TempDir(), "hello-1.0.tar.gz")
	makeTarGz(t, tarPath, map[string]string{
		"hello-1.0/configure": "#!/bin/sh\necho configure\n",
		"hello-1.0/README":    "readme",
	})

	// Serve the tarball over HTTP.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, tarPath)
	}))
	defer srv.Close()

	repo := NewFetchRepository()
	repo.SetCacheDir(cache)
	src, err := repo.FetchArchive(context.Background(), domain.ArchiveFetch{
		URL: srv.URL + "/hello-1.0.tar.gz",
	})
	require.NoError(t, err)
	// resolveSourceDir should have unwrapped the single nested dir.
	_, err = os.Stat(filepath.Join(src, "configure"))
	require.NoError(t, err, "configure should exist at %s", src)
	// Cache should hold the archive for reuse.
	_, err = os.Stat(filepath.Join(cache, "hello-1.0.tar.gz"))
	require.NoError(t, err)
}

func TestExecute_BinaryDownloadAndCopy(t *testing.T) {
	cache := t.TempDir()
	tarPath := filepath.Join(t.TempDir(), "tools.tar.gz")
	makeTarGz(t, tarPath, map[string]string{
		"tools/pgmetrics": "binary-bytes",
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, tarPath)
	}))
	defer srv.Close()

	f := fetch.NewService(NewFetchRepository())
	b := builder.NewService(NewBuildRepository())
	e := NewExecutor(f, b)
	e.cacheDir = cache

	dest := filepath.Join(t.TempDir(), "usr", "local", "bin", "pgmetrics")
	step := domain.Step{
		Name: "metrics",
		Kind: domain.StepKindBinary,
		Binary: &domain.BinaryStep{
			Fetch: &domain.FetchSpec{
				Type:    domain.FetchTypeArchive,
				Archive: &domain.ArchiveFetch{URL: srv.URL + "/tools.tar.gz"},
			},
			Install: &domain.BinaryInstall{
				Copy: []domain.CopySpec{{From: "tools/pgmetrics", To: dest, Mode: "0755"}},
			},
		},
	}
	res, err := e.Execute(context.Background(), step, domain.StepContext{})
	require.NoError(t, err)
	assert.Equal(t, domain.StepStatusSuccess, res.Status)

	data, err := os.ReadFile(dest)
	require.NoError(t, err)
	assert.Equal(t, "binary-bytes", string(data))
	fi, err := os.Stat(dest)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o755), fi.Mode().Perm())
}

func TestFetchArchive_ChecksumMismatch(t *testing.T) {
	cache := t.TempDir()
	tarPath := filepath.Join(t.TempDir(), "x.tar.gz")
	makeTarGz(t, tarPath, map[string]string{"x/file": "data"})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, tarPath)
	}))
	defer srv.Close()

	repo := NewFetchRepository()
	repo.SetCacheDir(cache)
	_, err := repo.FetchArchive(context.Background(), domain.ArchiveFetch{
		URL:          srv.URL + "/x.tar.gz",
		ChecksumType: "sha256",
		Checksum:     "deadbeef",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "checksum mismatch")
}
