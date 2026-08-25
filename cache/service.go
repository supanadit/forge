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
// name, content hash, status, any reusable outputs (SourceDir/Prefix), and the
// automatically discovered filesystem paths (Outputs) the step produced, so a
// later run can verify — and, via artifacts, restore — a cached result
// without re-executing the step.
type CacheFile struct {
	Name      string            `json:"name"`
	Key       string            `json:"key"`
	Status    string            `json:"status"`
	SourceDir string            `json:"source_dir,omitempty"`
	Prefix    string            `json:"prefix,omitempty"`
	Deps      map[string]string `json:"deps,omitempty"`
	Outputs   []string          `json:"outputs,omitempty"`
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

// Prune removes cache files and artifact tarballs for steps that no longer
// exist in the manifest, keeping the cache directory clean without deleting
// other projects' entries. A nil validSteps removes everything for the
// project.
func (s *Service) Prune(project string, validSteps map[string]bool) error {
	dir := s.projectDir(project)
	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := stepNameFromFile(e.Name())
		if validSteps == nil || !validSteps[name] {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}

	artDir := s.artifactDir(project)
	artEntries, err := os.ReadDir(artDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range artEntries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), artifactSuffix) {
			continue
		}
		base := strings.TrimSuffix(e.Name(), artifactSuffix)
		name := base
		if j := strings.LastIndex(base, "."); j >= 0 {
			name = base[:j]
		}
		if validSteps == nil || !validSteps[name] {
			_ = os.Remove(filepath.Join(artDir, e.Name()))
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

// Verify checks whether a cached step's outputs are still present on disk, so
// a cache hit is only trusted if the step's effects actually exist (e.g. in
// Docker where a layer rebuild may have removed installed files).
//
// Verification is inferred — no manual cache declarations:
//   - If the entry recorded Outputs (auto-discovered shell side effects),
//     those paths are authoritative.
//   - Otherwise the install op (if any) verifies via its declared outputs:
//     apt via dpkg's package database, source via Verify paths / prefix,
//     binary via copy destinations.
func Verify(step domain.Step, cf CacheFile) bool {
	if len(cf.Outputs) > 0 {
		return pathsExist(cf.Outputs)
	}
	for _, op := range step.Ops {
		if op.Install == nil {
			continue
		}
		return installVerified(op.Install)
	}
	return false
}

func installVerified(inst *domain.InstallOp) bool {
	if inst == nil {
		return false
	}
	if inst.Apt != nil {
		// apt installs verify via dpkg's package database: build + runtime.
		packages := append([]string{}, inst.Apt.Build...)
		packages = append(packages, inst.Apt.Runtime...)
		for _, pkg := range packages {
			if !dpkgInstalled(pkg) {
				return false
			}
		}
		return true
	}
	if inst.Source != nil {
		if len(inst.Source.Verify) > 0 {
			return allFilesExist(verifyPaths(inst.Source.Verify))
		}
		if inst.Source.Prefix != "" {
			return dirNonEmpty(inst.Source.Prefix)
		}
		return false
	}
	if inst.Binary != nil && len(inst.Binary.Copy) > 0 {
		dests := make([]string, 0, len(inst.Binary.Copy))
		for _, c := range inst.Binary.Copy {
			dests = append(dests, c.To)
		}
		return allFilesExist(dests)
	}
	return false
}

// OutputPaths returns the filesystem paths a successful step is expected to
// produce, used both for cache verification and for artifact persistence.
// Raw-shell ops return nil here — their outputs come from the automatic
// snapshot diff and travel through CacheFile.Outputs instead.
func OutputPaths(step domain.Step) []string {
	for _, op := range step.Ops {
		if op.Install == nil {
			continue
		}
		inst := op.Install
		if inst.Source != nil {
			var out []string
			out = append(out, verifyPaths(inst.Source.Verify)...)
			if inst.Source.Prefix != "" {
				out = append(out, inst.Source.Prefix)
			}
			return out
		}
		if inst.Binary != nil {
			var out []string
			for _, c := range inst.Binary.Copy {
				out = append(out, c.To)
			}
			return out
		}
		return nil // apt installs have no static output paths
	}
	return nil
}

// Cacheable reports whether a step can be cached at all (independent of
// whether a valid cache entry currently exists). A step is cacheable if it
// has operations.
func Cacheable(step domain.Step) bool {
	return len(step.Ops) > 0
}

func verifyPaths(checks []domain.VerifyCheck) []string {
	if len(checks) == 0 {
		return nil
	}
	out := make([]string, 0, len(checks))
	for _, c := range checks {
		out = append(out, c.File)
	}
	return out
}

func pathsExist(paths []string) bool {
	return allFilesExist(paths)
}

func allFilesExist(paths []string) bool {
	for _, p := range paths {
		if !fileExists(p) {
			return false
		}
	}
	return true
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
