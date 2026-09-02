// Package disk is the real driver for forge's repository interfaces. It
// operates on the local filesystem and executes system commands. Each module
// implementation lives in its own file within this single package.
package disk

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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

// Load reads and resolves the manifest at path, splicing inline includes.
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

	res := resolver{baseDir: filepath.Dir(abs)}
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
	Project    projectDTO        `toml:"project"`
	Vars       map[string]string `toml:"vars"`
	Includes   []includeDTO      `toml:"includes"`
	Components []componentsDTO   `toml:"components"`
}

type projectDTO struct {
	Name        string `toml:"name"`
	Description string `toml:"description"`
	NoCache     bool   `toml:"no_cache"`
}

type includeDTO struct {
	Path string `toml:"path"`
}

// componentsDTO is the raw TOML representation of a component. A component is
// an ordered `ops` list that is the entire build lifecycle.
type componentsDTO struct {
	Name  string         `toml:"name"`
	Needs []string       `toml:"needs"`
	Ops   []operationDTO `toml:"ops"`
}

type operationDTO struct {
	Raw      string       `toml:"raw"`
	User     *userOpDTO   `toml:"user"`
	Mkdir    []mkdirOpDTO `toml:"mkdir"`
	Chown    []chownOpDTO `toml:"chown"`
	Chmod    []chmodOpDTO `toml:"chmod"`
	Copy     []copyDTO    `toml:"copy"`
	Touch    []string     `toml:"touch"`
	Packages *packagesOpDTO   `toml:"packages"`
	Source   *sourceInstallDTO `toml:"source_install"`
	Binary   *binaryInstallDTO `toml:"binary_install"`
	Verify   []verifyCheckDTO `toml:"verify"`
	Generate *generateOpDTO `toml:"generate"`
}

// packagesOpDTO is the OS-agnostic package install. It accepts two forms:
// a plain array (all packages are runtime, kept after the build) or a table
// with build/runtime/remove classification and conditional packages.
type packagesOpDTO struct {
	Build       []string            `toml:"build"`
	Runtime     []string            `toml:"runtime"`
	Remove      []string            `toml:"remove"`
	Conditional []conditionalAptDTO `toml:"conditional"`
}

func (p *packagesOpDTO) UnmarshalTOML(data []byte) error {
	// data is the full inline value, e.g. `["curl", "git"]` or
	// `{ build = [...], runtime = [...] }`. Wrap it so go-toml can decode.
	var arr struct {
		Packages []string `toml:"packages"`
	}
	if err := toml.Unmarshal([]byte("packages = "+string(data)), &arr); err == nil && arr.Packages != nil {
		p.Runtime = arr.Packages
		return nil
	}
	var tab struct {
		Packages packagesOpDTO `toml:"packages"`
	}
	if err := toml.Unmarshal([]byte("packages = "+string(data)), &tab); err != nil {
		return err
	}
	*p = tab.Packages
	return nil
}

type conditionalAptDTO struct {
	Category string              `toml:"category"`
	When     versionConditionDTO `toml:"when"`
	Packages []string            `toml:"packages"`
}

type versionConditionDTO struct {
	Var string `toml:"var"`
	Gte string `toml:"gte"`
	Lte string `toml:"lte"`
	Gt  string `toml:"gt"`
	Lt  string `toml:"lt"`
	Eq  string `toml:"eq"`
}

type sourceInstallDTO struct {
	Type          string            `toml:"type"`
	URL           string            `toml:"url"`
	Ref           string            `toml:"ref"`
	Strategy      string            `toml:"strategy"`
	Flags         []string          `toml:"flags"`
	Prefix        string            `toml:"prefix"`
	Jobs          int               `toml:"jobs"`
	InstallTarget string            `toml:"install_target"`
	Env           map[string]string `toml:"env"`
	Verify        []verifyCheckDTO  `toml:"verify"`
	Before        []operationDTO    `toml:"before"`
	After         []operationDTO    `toml:"after"`
}

type binaryInstallDTO struct {
	URL  string    `toml:"url"`
	Copy []copyDTO `toml:"copy"`
}

type copyDTO struct {
	From string `toml:"from"`
	To   string `toml:"to"`
	Mode string `toml:"mode"`
}

type verifyCheckDTO struct {
	File string `toml:"file"`
}

type generateOpDTO struct {
	Tool  string   `toml:"tool"`
	Input string   `toml:"input"`
	Out   string   `toml:"out"`
	Flags []string `toml:"flags"`
}

type userOpDTO struct {
	Name       string `toml:"name"`
	CreateHome bool   `toml:"create_home"`
	System     bool   `toml:"system"`
	Shell      string `toml:"shell"`
}

type mkdirOpDTO struct {
	Path  string `toml:"path"`
	Mode  string `toml:"mode"`
	Owner string `toml:"owner"`
}

type chownOpDTO struct {
	Path      string `toml:"path"`
	Owner     string `toml:"owner"`
	Group     string `toml:"group"`
	Recursive bool   `toml:"recursive"`
}

type chmodOpDTO struct {
	Path string `toml:"path"`
	Mode string `toml:"mode"`
}

// strictErr rewrites go-toml's strict-mode error (unknown fields) into a
// message naming the offending keys, e.g. "unknown field(s): \"install\"".
func strictErr(err error) error {
	var se *toml.StrictMissingError
	if !errors.As(err, &se) {
		return err
	}
	var parts []string
	for i := range se.Errors {
		parts = append(parts, fmt.Sprintf("%q", strings.Join(se.Errors[i].Key(), ".")))
	}
	if len(parts) == 0 {
		return err
	}
	return fmt.Errorf("unknown field(s): %s", strings.Join(parts, ", "))
}

// parsedDoc is the resolved result of a single manifest file.
type parsedDoc struct {
	Project  domain.Project
	Vars     map[string]string
	Steps    []domain.Step
	Includes []string
}

// resolver resolves includes recursively.
type resolver struct {
	baseDir string
}

// resolveFile parses a single manifest file and builds its component list,
// splicing inline includes in order.
func (rs *resolver) resolveFile(ctx context.Context, path string) (parsedDoc, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return parsedDoc{}, err
	}
	var dto manifestDTO
	if err := toml.NewDecoder(bytes.NewReader(raw)).EnableUnmarshalerInterface().DisallowUnknownFields().Decode(&dto); err != nil {
		return parsedDoc{}, fmt.Errorf("parse %s: %w", path, strictErr(err))
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

	// Inline includes splice their components in order.
	for _, inc := range dto.Includes {
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

	for _, c := range dto.Components {
		step, err := translateComponent(c)
		if err != nil {
			return parsedDoc{}, err
		}
		doc.Steps = append(doc.Steps, step)
	}

	return doc, nil
}

// includePath resolves an include path relative to the including file's dir.
func (rs *resolver) includePath(parent, inc string) (string, error) {
	if filepath.IsAbs(inc) {
		return inc, nil
	}
	return filepath.Join(filepath.Dir(parent), inc), nil
}

// translateComponent converts a component-oriented [[components]] entry into a
// domain.Step whose ops list is the entire lifecycle.
func translateComponent(c componentsDTO) (domain.Step, error) {
	st := domain.Step{
		Name:      c.Name,
		DependsOn: c.Needs,
	}
	ops, err := mapOperations(c.Ops)
	if err != nil {
		return domain.Step{}, err
	}
	if err := validateOpsPackages(ops, c.Name); err != nil {
		return domain.Step{}, err
	}
	st.Ops = ops
	return st, nil
}

// validateOpsPackages rejects invalid package configurations in a step: a
// package appearing in more than one of build/runtime/remove, a conditional
// entry whose category is not "build" or "runtime", or a conditional package
// that is not listed in its category.
func validateOpsPackages(ops []domain.Operation, name string) error {
	for _, op := range ops {
		if op.Packages == nil {
			continue
		}
		p := op.Packages
		build := map[string]bool{}
		for _, pkg := range p.Build {
			build[pkg] = true
		}
		runtime := map[string]bool{}
		for _, pkg := range p.Runtime {
			runtime[pkg] = true
		}
		remove := map[string]bool{}
		for _, pkg := range p.Remove {
			remove[pkg] = true
		}
		for pkg := range build {
			if runtime[pkg] {
				return fmt.Errorf("step %q: package %q appears in both build and runtime", name, pkg)
			}
			if remove[pkg] {
				return fmt.Errorf("step %q: package %q appears in both build and remove", name, pkg)
			}
		}
		for pkg := range runtime {
			if remove[pkg] {
				return fmt.Errorf("step %q: package %q appears in both runtime and remove", name, pkg)
			}
		}
		for _, c := range p.Conditional {
			if c.Category != "build" && c.Category != "runtime" {
				return fmt.Errorf("step %q: package conditional category must be \"build\" or \"runtime\", got %q", name, c.Category)
			}
			allowed := build
			if c.Category == "runtime" {
				allowed = runtime
			}
			for _, pkg := range c.Packages {
				if !allowed[pkg] {
					return fmt.Errorf("step %q: conditional package %q is not listed in %s", name, pkg, c.Category)
				}
			}
		}
	}
	return nil
}

func mapOperations(dtos []operationDTO) ([]domain.Operation, error) {
	var out []domain.Operation
	for _, d := range dtos {
		op := domain.Operation{
			Raw:   d.Raw,
			Touch: d.Touch,
		}
		if d.User != nil {
			op.User = &domain.UserOp{Name: d.User.Name, CreateHome: d.User.CreateHome, System: d.User.System, Shell: d.User.Shell}
		}
		for _, m := range d.Mkdir {
			op.Mkdir = append(op.Mkdir, domain.MkdirOp{Path: m.Path, Mode: m.Mode, Owner: m.Owner})
		}
		for _, c := range d.Chown {
			op.Chown = append(op.Chown, domain.ChownOp{Path: c.Path, Owner: c.Owner, Group: c.Group, Recursive: c.Recursive})
		}
		for _, c := range d.Chmod {
			op.Chmod = append(op.Chmod, domain.ChmodOp{Path: c.Path, Mode: c.Mode})
		}
		for _, c := range d.Copy {
			op.Copy = append(op.Copy, domain.CopyOp{From: c.From, To: c.To, Mode: c.Mode})
		}
		if d.Packages != nil {
			op.Packages = mapPackages(d.Packages)
		}
		if d.Source != nil {
			op.SourceInstall = mapSourceInstall(d.Source)
		}
		if d.Binary != nil {
			op.BinaryInstall = mapBinaryInstall(d.Binary)
		}
		if len(d.Verify) > 0 {
			op.Verify = mapVerify(d.Verify)
		}
		if d.Generate != nil {
			op.Generate = &domain.GenerateOp{Tool: d.Generate.Tool, Input: d.Generate.Input, Out: d.Generate.Out, Flags: d.Generate.Flags}
		}
		out = append(out, op)
	}
	return out, nil
}

// mapOperationsOrNil maps operation DTOs to domain operations, returning nil
// when the list is empty.
func mapOperationsOrNil(dtos []operationDTO) []domain.Operation {
	if len(dtos) == 0 {
		return nil
	}
	ops, err := mapOperations(dtos)
	if err != nil {
		return nil
	}
	return ops
}

func mapPackages(d *packagesOpDTO) *domain.PackagesOp {
	if d == nil {
		return nil
	}
	po := &domain.PackagesOp{Build: d.Build, Runtime: d.Runtime, Remove: d.Remove}
	for _, c := range d.Conditional {
		po.Conditional = append(po.Conditional, domain.ConditionalApt{
			Category: c.Category,
			When:     domain.VersionCondition{Var: c.When.Var, Gte: c.When.Gte, Lte: c.When.Lte, Gt: c.When.Gt, Lt: c.When.Lt, Eq: c.When.Eq},
			Packages: c.Packages,
		})
	}
	return po
}

func mapSourceInstall(d *sourceInstallDTO) *domain.SourceInstall {
	if d == nil {
		return nil
	}
	return &domain.SourceInstall{
		Type: d.Type, URL: d.URL, Ref: d.Ref, Strategy: d.Strategy,
		Flags: d.Flags, Prefix: d.Prefix, Jobs: d.Jobs, InstallTarget: d.InstallTarget,
		Env: d.Env, Verify: mapVerify(d.Verify),
		Before: mapOperationsOrNil(d.Before),
		After:  mapOperationsOrNil(d.After),
	}
}

func mapBinaryInstall(d *binaryInstallDTO) *domain.BinaryInstall {
	if d == nil {
		return nil
	}
	bi := &domain.BinaryInstall{URL: d.URL}
	for _, c := range d.Copy {
		bi.Copy = append(bi.Copy, domain.CopySpec{From: c.From, To: c.To, Mode: c.Mode})
	}
	return bi
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