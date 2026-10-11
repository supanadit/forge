package disk

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"io"
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
		Ops: []domain.Operation{{BinaryInstall: &domain.BinaryInstall{
			URL:  srv.URL + "/tools.tar.gz",
			Copy: []domain.CopySpec{{From: "tools/pgmetrics", To: dest, Mode: "0755"}},
		}}},
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

func TestNormalizeChecksum(t *testing.T) {
	const hex64 = "a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2"
	const hex32 = "d41d8cd98f00b204e9800998ecf8427e"
	cases := map[string]string{
		hex64:                               hex64,
		"  " + hex64 + "  ":                 hex64,
		hex64 + "  postgresql-16.4.tar.gz":  hex64,
		hex64 + " *postgresql-16.4.tar.gz":  hex64,
		"sha256:" + hex64:                   hex64,
		"SHA256 (" + hex64 + ") = " + hex64: hex64,
		hex32 + "  file.tar.gz":             hex32,
		"":                                  "",
	}
	for in, want := range cases {
		assert.Equal(t, want, NormalizeChecksum(in), "input %q", in)
	}
}

// TestFetchArchive_AcceptsChecksumFileLine verifies a value pasted straight
// from an upstream .sha256 file ("<hex>  <filename>") is accepted.
func TestFetchArchive_AcceptsChecksumFileLine(t *testing.T) {
	cache := t.TempDir()
	tarPath := filepath.Join(t.TempDir(), "x.tar.gz")
	makeTarGz(t, tarPath, map[string]string{"x/file": "data"})
	sum, err := fileSHA256(tarPath)
	require.NoError(t, err)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, tarPath)
	}))
	defer srv.Close()

	repo := NewFetchRepository()
	repo.SetCacheDir(cache)
	dir, err := repo.FetchArchive(context.Background(), domain.ArchiveFetch{
		URL:      srv.URL + "/x.tar.gz",
		Checksum: sum + "  x.tar.gz",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, dir)
}

// TestFetchArchive_VerifiesCachedArchive verifies a corrupted archive already in
// the cache is caught rather than silently extracted.
func TestFetchArchive_VerifiesCachedArchive(t *testing.T) {
	cache := t.TempDir()
	tarPath := filepath.Join(t.TempDir(), "x.tar.gz")
	makeTarGz(t, tarPath, map[string]string{"x/file": "data"})
	sum, err := fileSHA256(tarPath)
	require.NoError(t, err)

	// Seed the cache with a corrupt file under the name the fetch expects.
	cached := filepath.Join(cache, "x.tar.gz")
	require.NoError(t, os.WriteFile(cached, []byte("corrupt"), 0o644))

	repo := NewFetchRepository()
	repo.SetCacheDir(cache)
	_, err = repo.FetchArchive(context.Background(), domain.ArchiveFetch{
		URL:          "https://example.invalid/x.tar.gz",
		Checksum:     sum,
		ChecksumType: "sha256",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "checksum mismatch")
}

// TestFetchArchive_MD5 verifies the md5 algorithm path.
func TestFetchArchive_MD5(t *testing.T) {
	cache := t.TempDir()
	tarPath := filepath.Join(t.TempDir(), "x.tar.gz")
	makeTarGz(t, tarPath, map[string]string{"x/file": "data"})
	sum, err := fileMD5(tarPath)
	require.NoError(t, err)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, tarPath)
	}))
	defer srv.Close()

	repo := NewFetchRepository()
	repo.SetCacheDir(cache)
	_, err = repo.FetchArchive(context.Background(), domain.ArchiveFetch{
		URL:          srv.URL + "/x.tar.gz",
		Checksum:     sum,
		ChecksumType: "md5",
	})
	require.NoError(t, err)

	// Wrong md5 must fail.
	repo2 := NewFetchRepository()
	repo2.SetCacheDir(t.TempDir())
	_, err = repo2.FetchArchive(context.Background(), domain.ArchiveFetch{
		URL:          srv.URL + "/x.tar.gz",
		Checksum:     "00000000000000000000000000000000",
		ChecksumType: "md5",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "checksum mismatch")
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func fileMD5(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
