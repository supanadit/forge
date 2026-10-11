package disk

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/supanadit/forge/domain"
)

// FetchRepository acquires source trees via git clone or archive download.
type FetchRepository struct {
	cacheDir string
	client   *http.Client
}

// NewFetchRepository creates a filesystem/networking-backed fetch repository.
func NewFetchRepository() *FetchRepository {
	return &FetchRepository{
		cacheDir: filepath.Join(os.TempDir(), "forge-cache"),
		client:   &http.Client{},
	}
}

// SetCacheDir overrides the default download/extract cache location.
func (r *FetchRepository) SetCacheDir(dir string) { r.cacheDir = dir }

// FetchArchive downloads and extracts a compressed archive, returning the
// resolved source directory (handling the single-nested-dir case).
func (r *FetchRepository) FetchArchive(ctx context.Context, spec domain.ArchiveFetch) (string, error) {
	if spec.URL == "" {
		return "", fmt.Errorf("fetch archive: empty url")
	}
	if err := os.MkdirAll(r.cacheDir, 0o755); err != nil {
		return "", err
	}

	// Download to cache if not present.
	archivePath := filepath.Join(r.cacheDir, filepath.Base(spec.URL))
	if _, err := os.Stat(archivePath); os.IsNotExist(err) {
		if err := r.download(ctx, spec.URL, archivePath); err != nil {
			return "", err
		}
	}

	// Verify checksum if requested. This also covers an archive already present in
	// the cache: a tampered or truncated cached file is caught, not silently
	// extracted.
	if spec.Checksum != "" {
		algo := spec.ChecksumType
		if algo == "" {
			algo = "sha256"
		}
		if err := r.verifyChecksum(archivePath, algo, spec.Checksum); err != nil {
			return "", fmt.Errorf("verify %s: %w", filepath.Base(archivePath), err)
		}
	}

	// Extract to a deterministic destination.
	dest := spec.Dest
	if dest == "" {
		base := strings.TrimSuffix(filepath.Base(spec.URL), filepath.Ext(filepath.Base(spec.URL)))
		dest = filepath.Join(r.cacheDir, base)
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return "", err
	}
	if err := extractArchive(archivePath, dest); err != nil {
		return "", err
	}

	return resolveSourceDir(dest), nil
}

// FetchGit clones a git repository, returning the resolved source directory.
func (r *FetchRepository) FetchGit(ctx context.Context, spec domain.GitFetch, verbose bool) (string, error) {
	if spec.URL == "" {
		return "", fmt.Errorf("fetch git: empty url")
	}
	if err := os.MkdirAll(r.cacheDir, 0o755); err != nil {
		return "", err
	}

	dest := spec.Dest
	if dest == "" {
		name := strings.TrimSuffix(filepath.Base(spec.URL), ".git")
		dest = filepath.Join(r.cacheDir, name)
	}

	if _, err := os.Stat(filepath.Join(dest, ".git")); os.IsNotExist(err) {
		args := []string{"clone"}
		if spec.Depth > 0 {
			args = append(args, "--depth", fmt.Sprintf("%d", spec.Depth))
		}
		if spec.Ref != "" {
			args = append(args, "-b", spec.Ref)
		}
		args = append(args, spec.URL, dest)
		cmd := exec.Command("git", args...)
		if verbose {
			out, e := runProcessVerbose(ctx, cmd, nil, nil)
			if e != nil {
				return "", fmt.Errorf("git clone %s: %w\n%s", spec.URL, e, out)
			}
		} else {
			if out, err := runProcess(ctx, cmd); err != nil {
				return "", fmt.Errorf("git clone %s: %w\n%s", spec.URL, err, out)
			}
		}
	}

	return resolveSourceDir(dest), nil
}

func (r *FetchRepository) download(ctx context.Context, url, dest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: status %d", url, resp.StatusCode)
	}
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, resp.Body); err != nil {
		return err
	}
	return nil
}

func (r *FetchRepository) verifyChecksum(path, algo, expected string) error {
	expected = NormalizeChecksum(expected)
	var h hash.Hash
	switch algo {
	case "sha256":
		h = sha256.New()
	case "md5":
		h = md5.New()
	default:
		return fmt.Errorf("unsupported checksum type %q", algo)
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, expected) {
		return fmt.Errorf("checksum mismatch: got %s want %s", got, expected)
	}
	return nil
}

// NormalizeChecksum extracts the bare hex digest from a checksum value. It
// accepts a bare digest or a line from a published checksum file, in either
// common form:
//
//	"<hex>  <filename>"   (coreutils sha256sum/md5sum)
//	"<algo>:<hex>"        (BSD-style, e.g. sha256:...)
//
// so a value can be pasted straight from upstream's .sha256/.md5 file. A value
// that appears to already be a bare digest is returned unchanged.
func NormalizeChecksum(value string) string {
	v := strings.TrimSpace(value)
	if v == "" {
		return ""
	}
	// BSD/OpenSSL-style "SHA256 (filename) = <hex>".
	if i := strings.LastIndex(v, "="); i >= 0 {
		candidate := strings.TrimSpace(v[i+1:])
		if isHex(candidate) && len(candidate) >= 32 {
			return candidate
		}
	}
	// BSD-style "algo:hex".
	if i := strings.LastIndex(v, ":"); i >= 0 {
		candidate := v[i+1:]
		if isHex(candidate) && len(candidate) >= 32 {
			return candidate
		}
	}
	// Take the first whitespace-separated field (drops a " *filename" suffix).
	if fields := strings.Fields(v); len(fields) > 0 {
		if isHex(fields[0]) {
			return fields[0]
		}
	}
	return v
}

// isHex reports whether s is non-empty and contains only hexadecimal digits.
func isHex(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f', r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}

// extractArchive extracts .tar.gz, .tgz, .tar, and .zip archives.
func extractArchive(path, dest string) error {
	switch {
	case strings.HasSuffix(path, ".tar.gz"), strings.HasSuffix(path, ".tgz"):
		return extractTarGz(path, dest)
	case strings.HasSuffix(path, ".tar"):
		return extractTar(path, dest)
	case strings.HasSuffix(path, ".zip"):
		return extractZip(path, dest)
	default:
		return fmt.Errorf("unsupported archive format: %s", path)
	}
}

func extractTarGz(path, dest string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	return untar(gz, dest)
}

func extractTar(path, dest string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return untar(f, dest)
}

func untar(r io.Reader, dest string) error {
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		target := filepath.Join(dest, hdr.Name)
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode))
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			out.Close()
		}
	}
	return nil
}

func extractZip(path, dest string) error {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, f := range zr.File {
		target := filepath.Join(dest, f.Name)
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, f.Mode())
		if err != nil {
			rc.Close()
			return err
		}
		if _, err := io.Copy(out, rc); err != nil {
			out.Close()
			rc.Close()
			return err
		}
		out.Close()
		rc.Close()
	}
	return nil
}

// resolveSourceDir finds the actual source directory: if the extract target
// contains a single nested directory (the common tarball case), return that.
func resolveSourceDir(dir string) string {
	if hasBuildFile(dir) {
		return dir
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return dir
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			dirs = append(dirs, e.Name())
		}
	}
	if len(dirs) == 1 {
		candidate := filepath.Join(dir, dirs[0])
		if hasBuildFile(candidate) {
			return candidate
		}
	}
	return dir
}

func hasBuildFile(dir string) bool {
	for _, name := range []string{"configure", "Configure", "CMakeLists.txt", "Makefile", "makefile", "meson.build"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return true
		}
	}
	return false
}
