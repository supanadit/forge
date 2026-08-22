package cache

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/supanadit/forge/domain"
)

// CacheFile is the persisted metadata for a cached step. It stores the step's
// name, content hash, status, and any reusable outputs (SourceDir/Prefix) so a
// later run can restore a cached result without re-executing the step.
type CacheFile struct {
	Name      string            `json:"name"`
	Key       string            `json:"key"`
	Status    string            `json:"status"`
	SourceDir string            `json:"source_dir,omitempty"`
	Prefix    string            `json:"prefix,omitempty"`
	Deps      map[string]string `json:"deps,omitempty"`
}

// Service manages the build cache: reading/writing cache files, validating
// cache hits, and pruning orphaned entries for a project.
type Service struct {
	dir string
}

// New creates a cache service rooted at dir. If dir is empty, it returns an
// error so callers can default to the standard location.
func New(dir string) (*Service, error) {
	if dir == "" {
		return nil, fmt.Errorf("cache dir is empty")
	}
	return &Service{dir: dir}, nil
}

// projectDir returns the cache directory for a project name.
func (s *Service) projectDir(project string) string {
	return filepath.Join(s.dir, project)
}

// StepPath returns the cache file path for a step name and key.
func (s *Service) StepPath(project, name, key string) string {
	return filepath.Join(s.projectDir(project), name+"."+key+".json")
}

// Lookup returns the cached result for a step, if present and matching the key.
func (s *Service) Lookup(project, name, key string) (CacheFile, bool) {
	path := s.StepPath(project, name, key)
	data, err := os.ReadFile(path)
	if err != nil {
		return CacheFile{}, false
	}
	var cf CacheFile
	if err := json.Unmarshal(data, &cf); err != nil {
		return CacheFile{}, false
	}
	if cf.Key != key || cf.Status != string(domain.StepStatusSuccess) {
		return CacheFile{}, false
	}
	return cf, true
}

// Save writes a cache file for a step.
func (s *Service) Save(project, name, key string, cf CacheFile) error {
	dir := s.projectDir(project)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	cf.Name = name
	cf.Key = key
	cf.Status = string(domain.StepStatusSuccess)
	data, err := json.MarshalIndent(cf, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.StepPath(project, name, key), data, 0o644)
}

// Prune removes cache files for steps that no longer exist in the manifest,
// keeping the cache directory clean without deleting other projects' entries.
func (s *Service) Prune(project string, validSteps map[string]bool) error {
	dir := s.projectDir(project)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := stepNameFromFile(e.Name())
		if !validSteps[name] {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
	return nil
}

// stepNameFromFile extracts the step name from "<name>.<key>.json".
func stepNameFromFile(filename string) string {
	if i := strings.LastIndex(filename, "."); i > 0 {
		// strip ".json", then strip ".<key>"
		base := filename[:i]
		if j := strings.LastIndex(base, "."); j >= 0 {
			return base[:j]
		}
		return base
	}
	return filename
}

// List returns the cache file paths for a project.
func (s *Service) List(project string) ([]string, error) {
	dir := s.projectDir(project)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	return out, nil
}

// Verify checks whether a step's output is still present on disk, so a cache
// hit is only trusted if the step's effects actually exist (e.g. in Docker
// where a layer rebuild may have removed installed files).
func Verify(step domain.Step) bool {
	switch step.Kind {
	case domain.StepKindApt:
		if step.Apt != nil && step.Apt.Action == "install" {
			for _, pkg := range step.Apt.Packages {
				if !dpkgInstalled(pkg) {
					return false
				}
			}
			return true
		}
		return false // remove/purge steps can't be verified; always re-run
	case domain.StepKindSource:
		if step.Source == nil {
			return false
		}
		checks := step.Source.CacheVerify
		if len(checks) == 0 {
			checks = step.Source.Verify
		}
		if len(checks) > 0 {
			for _, c := range checks {
				if !fileExists(c.File) {
					return false
				}
			}
			return true
		}
		// Fall back to the install prefix existing and non-empty.
		if step.Source.Build != nil && step.Source.Build.Prefix != "" {
			return dirNonEmpty(step.Source.Build.Prefix)
		}
		return false
	case domain.StepKindBinary:
		if step.Binary == nil {
			return false
		}
		checks := step.Binary.CacheVerify
		if len(checks) == 0 {
			checks = step.Binary.Verify
		}
		if len(checks) > 0 {
			for _, c := range checks {
				if !fileExists(c.File) {
					return false
				}
			}
			return true
		}
		if step.Binary.Install != nil {
			for _, c := range step.Binary.Install.Copy {
				if !fileExists(c.To) {
					return false
				}
			}
			return true
		}
		return false
	case domain.StepKindShell:
		if step.Shell == nil || len(step.Shell.CacheVerify) == 0 {
			return false // shell steps need explicit cache_verify to be cacheable
		}
		for _, c := range step.Shell.CacheVerify {
			if !fileExists(c.File) {
				return false
			}
		}
		return true
	default:
		return false // verify steps are cheap and always run
	}
}

// Cacheable reports whether a step can be cached at all (independent of whether
// a valid cache entry currently exists).
func Cacheable(step domain.Step) bool {
	switch step.Kind {
	case domain.StepKindApt:
		return step.Apt != nil && step.Apt.Action == "install"
	case domain.StepKindSource:
		return step.Source != nil &&
			(len(step.Source.CacheVerify) > 0 || len(step.Source.Verify) > 0 ||
				(step.Source.Build != nil && step.Source.Build.Prefix != ""))
	case domain.StepKindBinary:
		return step.Binary != nil &&
			(len(step.Binary.CacheVerify) > 0 || len(step.Binary.Verify) > 0 ||
				(step.Binary.Install != nil && len(step.Binary.Install.Copy) > 0))
	case domain.StepKindShell:
		return step.Shell != nil && len(step.Shell.CacheVerify) > 0
	default:
		return false
	}
}

func dpkgInstalled(pkg string) bool {
	cmd := exec.Command("dpkg", "-s", pkg)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "Status: install ok installed")
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func dirNonEmpty(path string) bool {
	entries, err := os.ReadDir(path)
	if err != nil {
		return false
	}
	return len(entries) > 0
}

// SortedDeps returns the cache keys of a step's dependencies in deterministic
// order, for embedding in the step's cache key.
func SortedDeps(step domain.Step, previous map[string]domain.StepResult) map[string]string {
	deps := make(map[string]string, len(step.DependsOn))
	for _, dep := range step.DependsOn {
		if res, ok := previous[dep]; ok {
			deps[dep] = res.CacheKey
		}
	}
	return deps
}
