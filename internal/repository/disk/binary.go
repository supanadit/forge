package disk

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"github.com/supanadit/forge/domain"
	"github.com/supanadit/forge/internal/repository"
)

// executeBinary downloads a prebuilt archive and copies files into place.
func (e *Executor) executeBinary(ctx context.Context, bin *domain.BinaryStep, lookup repository.Lookup) error {
	if bin == nil || bin.Fetch == nil {
		return fmt.Errorf("binary step has no fetch config")
	}
	if bin.Fetch.Type != domain.FetchTypeArchive {
		return fmt.Errorf("binary step requires an archive fetch, got %q", bin.Fetch.Type)
	}

	spec := *bin.Fetch.Archive
	spec.URL = repository.Replace(spec.URL, lookup)
	spec.Dest = repository.Replace(spec.Dest, lookup)

	extractDir, err := e.fetchService.FetchArchive(ctx, spec)
	if err != nil {
		return err
	}

	if bin.Install == nil {
		return fmt.Errorf("binary step has no install config")
	}
	for _, c := range bin.Install.Copy {
		src := filepath.Join(extractDir, c.From)
		dst := repository.Replace(c.To, lookup)
		if err := copyFile(src, dst); err != nil {
			return fmt.Errorf("copy %s -> %s: %w", c.From, dst, err)
		}
		if c.Mode != "" {
			mode, err := strconv.ParseUint(c.Mode, 8, 32)
			if err != nil {
				return fmt.Errorf("parse mode %q: %w", c.Mode, err)
			}
			if err := os.Chmod(dst, os.FileMode(mode)); err != nil {
				return err
			}
		}
	}

	return e.executeVerify(bin.Verify, lookup)
}

// copyFile copies a file (creating parent dirs), preserving nothing but bytes.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return nil
}
