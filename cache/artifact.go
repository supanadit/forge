package cache

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// artifactSuffix is the file extension of persisted artifact archives.
const artifactSuffix = ".tar.gz"

// artifactDir returns the directory holding a project's artifact archives:
// <cache-dir>/artifacts/<project>.
func (s *Service) artifactDir(project string) string {
	return filepath.Join(s.dir, "artifacts", project)
}

// ArtifactPath returns the artifact archive path for a step and key.
func (s *Service) ArtifactPath(project, name, key string) string {
	return filepath.Join(s.artifactDir(project), name+"."+key+artifactSuffix)
}

// SaveArtifact tars the given absolute paths into
// <cache-dir>/artifacts/<project>/<name>.<key>.tar.gz. Paths are stored
// relative to "/" so extraction restores them at their original locations,
// preserving modes, ownership (when run as root), and symlinks. The archive
// is written to a temp file and renamed for atomicity.
func (s *Service) SaveArtifact(project, name, key string, paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	out := s.ArtifactPath(project, name, key)
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}

	var rels []string
	seen := make(map[string]bool, len(paths))
	for _, p := range paths {
		if p == "" || p == "/" {
			continue
		}
		cleaned := filepath.Clean(p)
		if !filepath.IsAbs(cleaned) || seen[cleaned] {
			continue
		}
		seen[cleaned] = true
		rels = append(rels, cleaned[1:]) // strip leading "/" for tar -C /
	}

	tmp := out + ".tmp"
	defer os.Remove(tmp)
	args := append([]string{"czf", tmp, "-C", "/"}, rels...)
	cmd := exec.Command("tar", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("tar artifacts for %s/%s: %w: %s", project, name, err, stderr.String())
	}
	return os.Rename(tmp, out)
}

// RestoreArtifact extracts a step's artifact archive back into "/". Returns an
// error when no archive exists — callers treat any error as "nothing to
// restore" and fall through to re-execution.
func (s *Service) RestoreArtifact(project, name, key string) error {
	path := s.ArtifactPath(project, name, key)
	if _, err := os.Stat(path); err != nil {
		return err
	}
	cmd := exec.Command("tar", "xzf", path, "-C", "/")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("restore artifacts for %s/%s: %w: %s", project, name, err, stderr.String())
	}
	return nil
}
