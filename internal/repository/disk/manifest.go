// Package disk is the real driver for forge's repository interfaces. It
// operates on the local filesystem and executes system commands. Each module
// implementation lives in its own file within this single package.
package disk

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/supanadit/forge/domain"
)

// ManifestRepository loads manifests from the filesystem as TOML.
type ManifestRepository struct{}

// NewManifestRepository creates a filesystem-backed manifest repository.
func NewManifestRepository() *ManifestRepository {
	return &ManifestRepository{}
}

// Load reads and resolves the manifest at path, splicing inline includes and
// expanding named include groups referenced via the `use` field.
func (r *ManifestRepository) Load(ctx context.Context, path string) (domain.Manifest, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return domain.Manifest{}, err
	}
	if _, err := os.Stat(abs); err != nil {
		if os.IsNotExist(err) {
			return domain.Manifest{}, fmt.Errorf("%w: %s", domain.ErrManifestNotFound, path)
		}
		return domain.Manifest{}, err
	}

	res := resolver{baseDir: filepath.Dir(abs), groups: map[string][]domain.Step{}}
	doc, err := res.resolveFile(ctx, abs)
	if err != nil {
		return domain.Manifest{}, fmt.Errorf("%w: %v", domain.ErrInvalidManifest, err)
	}

	return domain.Manifest{
		Project:  doc.Project,
		Vars:     doc.Vars,
		Steps:    doc.Steps,
		Includes: doc.Includes,
	}, nil
}

// manifestDTO is the raw TOML representation of a forge manifest.
type manifestDTO struct {
	Project  projectDTO        `toml:"project"`
	Vars     map[string]string `toml:"vars"`
	Includes []includeDTO      `toml:"includes"`
	Steps    []stepDTO         `toml:"steps"`
}

type projectDTO struct {
	Name        string `toml:"name"`
	Description string `toml:"description"`
	NoCache     bool   `toml:"no_cache"`
}

type includeDTO struct {
	Path string            `toml:"path"`
	As   string            `toml:"as"`
	Vars map[string]string `toml:"vars"`
}

type stepDTO struct {
	Name      string   `toml:"name"`
	DependsOn []string `toml:"depends_on"`
	Run       string   `toml:"run"`
	Use       string   `toml:"use"`

	// apt
	Action              string                `toml:"action"`
	Packages            []string              `toml:"packages"`
	PackagesConditional []conditionalPackages `toml:"packages_conditional"`

	// fetch (source + binary)
	Fetch *fetchDTO `toml:"fetch"`

	// source build
	Build   *buildDTO         `toml:"build"`
	Install *installDTO       `toml:"install"`
	From    string            `toml:"from"`
	Dir     string            `toml:"dir"`
	Env     map[string]string `toml:"env"`

	// shell
	Commands []string `toml:"commands"`

	// verify
	Checks []verifyCheckDTO `toml:"checks"`
	Verify []verifyCheckDTO `toml:"verify"`
}

// installDTO accepts either `install = true` (source steps) or
// `install = { copy = [...] }` (binary steps) via a custom unmarshaler.
type installDTO struct {
	Enabled bool
	Copy    []copyDTO
}

func (i *installDTO) UnmarshalTOML(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if b, err := strconv.ParseBool(trimmed); err == nil {
		i.Enabled = b
		return nil
	}
	var raw struct {
		Val struct {
			Copy []copyDTO `toml:"copy"`
		} `toml:"val"`
	}
	if err := toml.Unmarshal([]byte("val = "+trimmed), &raw); err != nil {
		return err
	}
	i.Enabled = len(raw.Val.Copy) > 0
	i.Copy = raw.Val.Copy
	return nil
}

type conditionalPackages struct {
	Condition string   `toml:"condition"`
	Packages  []string `toml:"packages"`
}

type fetchDTO struct {
	Type string `toml:"type"`

	// archive
	URL          string `toml:"url"`
	ChecksumType string `toml:"checksum_type"`
	Checksum     string `toml:"checksum"`
	Dest         string `toml:"dest"`

	// git
	Ref   string `toml:"ref"`
	Depth int    `toml:"depth"`
}

type buildDTO struct {
	Strategy      string            `toml:"strategy"`
	Prefix        string            `toml:"prefix"`
	Flags         []string          `toml:"flags"`
	Env           map[string]string `toml:"env"`
	MakeFlags     []string          `toml:"make_flags"`
	Jobs          int               `toml:"jobs"`
	InstallTarget string            `toml:"install_target"`
}

type copyDTO struct {
	From string `toml:"from"`
	To   string `toml:"to"`
	Mode string `toml:"mode"`
}

type verifyCheckDTO struct {
	File string `toml:"file"`
}

// parsedDoc is the resolved result of a single manifest file.
type parsedDoc struct {
	Project  domain.Project
	Vars     map[string]string
	Steps    []domain.Step
	Includes []string
}

// resolver resolves includes recursively and maintains the named-group
// registry for the `use` field.
type resolver struct {
	baseDir string
	groups  map[string][]domain.Step
}

// resolveFile parses a single manifest file, registers its named include
// groups, and builds its step list (splicing inline includes and `use` groups).
func (rs *resolver) resolveFile(ctx context.Context, path string) (parsedDoc, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return parsedDoc{}, err
	}
	// Legacy manifests may still carry cache_verify; it is ignored — forge
	// infers step outputs automatically. Warn once per file so authors can
	// clean up.
	if bytes.Contains(raw, []byte("cache_verify")) {
		fmt.Fprintf(os.Stderr, "warning: %s: 'cache_verify' is deprecated and ignored; forge infers step outputs automatically\n", path)
	}
	var dto manifestDTO
	if err := toml.NewDecoder(bytes.NewReader(raw)).EnableUnmarshalerInterface().Decode(&dto); err != nil {
		return parsedDoc{}, fmt.Errorf("parse %s: %w", path, err)
	}

	doc := parsedDoc{
		Project: domain.Project{
			Name:        dto.Project.Name,
			Description: dto.Project.Description,
			NoCache:     dto.Project.NoCache,
		},
		Vars:     dto.Vars,
		Includes: []string{path},
	}

	// Register named groups first so `use` references resolve regardless of
	// declaration order within the file.
	for _, inc := range dto.Includes {
		if inc.As == "" {
			continue
		}
		incPath, err := rs.includePath(path, inc.Path)
		if err != nil {
			return parsedDoc{}, err
		}
		sub, err := rs.resolveFile(ctx, incPath)
		if err != nil {
			return parsedDoc{}, err
		}
		rs.groups[inc.As] = sub.Steps
		doc.Includes = append(doc.Includes, sub.Includes...)
	}

	// Build the ordered step list: inline includes first (positional splice),
	// then this file's own steps (with `use` groups expanded).
	for _, inc := range dto.Includes {
		if inc.As != "" {
			continue
		}
		incPath, err := rs.includePath(path, inc.Path)
		if err != nil {
			return parsedDoc{}, err
		}
		sub, err := rs.resolveFile(ctx, incPath)
		if err != nil {
			return parsedDoc{}, err
		}
		doc.Steps = append(doc.Steps, sub.Steps...)
		doc.Includes = append(doc.Includes, sub.Includes...)
	}

	for _, sd := range dto.Steps {
		if sd.Use != "" {
			groupSteps, ok := rs.groups[sd.Use]
			if !ok {
				return parsedDoc{}, fmt.Errorf("%w: %q", domain.ErrUnknownIncludeGroup, sd.Use)
			}
			doc.Steps = append(doc.Steps, groupSteps...)
			continue
		}
		step, err := mapStep(sd)
		if err != nil {
			return parsedDoc{}, err
		}
		doc.Steps = append(doc.Steps, step)
	}

	return doc, nil
}

// includePath resolves an include path relative to the including file's dir.
// The resolver's baseDir is only a fallback when the parent path is not
// available (should not happen in practice).
func (rs *resolver) includePath(parent, inc string) (string, error) {
	if filepath.IsAbs(inc) {
		return inc, nil
	}
	return filepath.Join(filepath.Dir(parent), inc), nil
}

// mapStep converts a TOML stepDTO into a domain.Step.
func mapStep(d stepDTO) (domain.Step, error) {
	st := domain.Step{
		Name:      d.Name,
		DependsOn: d.DependsOn,
		Use:       d.Use,
	}

	switch domain.StepKind(d.Run) {
	case domain.StepKindApt:
		st.Kind = domain.StepKindApt
		st.Apt = &domain.AptStep{
			Action:   d.Action,
			Packages: d.Packages,
		}
		for _, cp := range d.PackagesConditional {
			st.Apt.PackagesConditional = append(st.Apt.PackagesConditional, domain.ConditionalPackages{
				Condition: cp.Condition,
				Packages:  cp.Packages,
			})
		}
	case domain.StepKindSource:
		st.Kind = domain.StepKindSource
		fetch, err := mapFetch(d.Fetch)
		if err != nil {
			return domain.Step{}, err
		}
		build, err := mapBuild(d.Build)
		if err != nil {
			return domain.Step{}, err
		}
		st.Source = &domain.SourceStep{
			Fetch:   fetch,
			Build:   build,
			Install: d.Install != nil && d.Install.Enabled,
			From:    d.From,
			Dir:     d.Dir,
			Env:     d.Env,
			Verify:  mapVerify(d.Verify),
		}
	case domain.StepKindBinary:
		st.Kind = domain.StepKindBinary
		fetch, err := mapFetch(d.Fetch)
		if err != nil {
			return domain.Step{}, err
		}
		st.Binary = &domain.BinaryStep{
			Fetch:  fetch,
			Verify: mapVerify(d.Verify),
		}
		if d.Install != nil {
			bi := &domain.BinaryInstall{}
			for _, c := range d.Install.Copy {
				bi.Copy = append(bi.Copy, domain.CopySpec{From: c.From, To: c.To, Mode: c.Mode})
			}
			st.Binary.Install = bi
		}
	case domain.StepKindShell:
		st.Kind = domain.StepKindShell
		st.Shell = &domain.ShellStep{
			Commands: d.Commands,
			Env:      d.Env,
			Dir:      d.Dir,
			Verify:   mapVerify(d.Verify),
		}
	case domain.StepKindVerify:
		st.Kind = domain.StepKindVerify
		st.Verify = &domain.VerifyStep{Checks: mapVerify(d.Checks)}
	default:
		return domain.Step{}, fmt.Errorf("step %q: %w: %q", d.Name, domain.ErrUnknownStepKind, d.Run)
	}

	return st, nil
}

func mapFetch(d *fetchDTO) (*domain.FetchSpec, error) {
	if d == nil {
		return nil, nil
	}
	spec := &domain.FetchSpec{Type: domain.FetchType(d.Type)}
	switch spec.Type {
	case domain.FetchTypeArchive:
		spec.Archive = &domain.ArchiveFetch{
			URL:          d.URL,
			ChecksumType: d.ChecksumType,
			Checksum:     d.Checksum,
			Dest:         d.Dest,
		}
	case domain.FetchTypeGit:
		spec.Git = &domain.GitFetch{
			URL:   d.URL,
			Ref:   d.Ref,
			Depth: d.Depth,
			Dest:  d.Dest,
		}
	default:
		return nil, fmt.Errorf("unknown fetch type %q", d.Type)
	}
	return spec, nil
}

func mapBuild(d *buildDTO) (*domain.BuildSpec, error) {
	if d == nil {
		return nil, nil
	}
	return &domain.BuildSpec{
		Strategy:      domain.BuildStrategy(d.Strategy),
		Prefix:        d.Prefix,
		Flags:         d.Flags,
		Env:           d.Env,
		MakeFlags:     d.MakeFlags,
		Jobs:          d.Jobs,
		InstallTarget: d.InstallTarget,
	}, nil
}

func mapVerify(d []verifyCheckDTO) []domain.VerifyCheck {
	if len(d) == 0 {
		return nil
	}
	out := make([]domain.VerifyCheck, 0, len(d))
	for _, v := range d {
		out = append(out, domain.VerifyCheck{File: v.File})
	}
	return out
}
