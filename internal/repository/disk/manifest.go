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
	Components []componentsDTO `toml:"components"`
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

	// install (universal: apt / source / binary)
	Install *installOpDTO `toml:"install"`

	// shell
	Commands []string          `toml:"commands"`
	Env      map[string]string `toml:"env"`
	Dir      string            `toml:"dir"`

	// verify
	Checks []verifyCheckDTO `toml:"checks"`
	Verify []verifyCheckDTO `toml:"verify"`
}

// componentsDTO is the raw TOML representation of a component-oriented step.
// It mirrors stepDTO but uses `needs` (instead of `depends_on`) and an ordered
// `ops` list that is the entire lifecycle.
type componentsDTO struct {
	Name  string   `toml:"name"`
	Needs []string `toml:"needs"`
	Ops   []operationDTO `toml:"ops"`
}

// installOpDTO is the polymorphic universal install operation: exactly one of
// apt, source, or binary is set.
type installOpDTO struct {
	Apt    *aptInstallDTO    `toml:"apt"`
	Source *sourceInstallDTO `toml:"source"`
	Binary *binaryInstallDTO `toml:"binary"`
}

func (i *installOpDTO) UnmarshalTOML(data []byte) error {
	// data is the full inline table WITH braces, e.g. `{ type = "git", ... }`.
	// Wrap it as a valid table so go-toml can decode it.
	var raw struct {
		Install struct {
			Apt    *aptInstallDTO    `toml:"apt"`
			Source *sourceInstallDTO `toml:"source"`
			Binary *binaryInstallDTO `toml:"binary"`
		} `toml:"install"`
	}
	if err := toml.Unmarshal([]byte("install = "+string(data)), &raw); err == nil {
		i.Apt = raw.Install.Apt
		i.Source = raw.Install.Source
		i.Binary = raw.Install.Binary
		return nil
	}
	// Flat form: `{ type = "archive", source = "...", strategy = "..." }`.
	// Here `source` is the plain URL string at the top level, not a nested
	// table, so the nested decode above fails. Map the whole inline table onto
	// the source install instead.
	var flat struct {
		Install sourceInstallDTO `toml:"install"`
	}
	if err := toml.Unmarshal([]byte("install = "+string(data)), &flat); err != nil {
		return err
	}
	i.Source = &flat.Install
	return nil
}

type aptInstallDTO struct {
	Build       []string            `toml:"build"`
	Runtime     []string            `toml:"runtime"`
	Conditional []conditionalAptDTO `toml:"conditional"`
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
	Source        string            `toml:"source"`
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
	Source string    `toml:"source"`
	Copy   []copyDTO `toml:"copy"`
}

type copyDTO struct {
	From string `toml:"from"`
	To   string `toml:"to"`
	Mode string `toml:"mode"`
}

type verifyCheckDTO struct {
	File string `toml:"file"`
}

type operationDTO struct {
	Raw      string       `toml:"raw"`
	User     *userOpDTO   `toml:"user"`
	Mkdir    []mkdirOpDTO `toml:"mkdir"`
	Chown    []chownOpDTO `toml:"chown"`
	Chmod    []chmodOpDTO `toml:"chmod"`
	Copy     []copyDTO    `toml:"copy"`
	Touch    []string     `toml:"touch"`
	Apt      *aptOpDTO    `toml:"apt"`
	Install  *installOpDTO  `toml:"install"`
	Verify   []verifyCheckDTO `toml:"verify"`
	Generate *generateOpDTO `toml:"generate"`
}

type generateOpDTO struct {
	Tool  string   `toml:"tool"`
	Input string   `toml:"input"`
	Out   string   `toml:"out"`
	Flags []string `toml:"flags"`
}

type aptOpDTO struct {
	Action   string   `toml:"action"`
	Packages []string `toml:"packages"`
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
// The resolver's baseDir is only a fallback when the parent path is not
// available (should not happen in practice).
func (rs *resolver) includePath(parent, inc string) (string, error) {
	if filepath.IsAbs(inc) {
		return inc, nil
	}
	return filepath.Join(filepath.Dir(parent), inc), nil
}

// mapStep converts a legacy TOML stepDTO into a domain.Step, mapping the
// kind-specific fields onto the ordered ops model.
func mapStep(d stepDTO) (domain.Step, error) {
	st := domain.Step{
		Name:      d.Name,
		DependsOn: d.DependsOn,
	}

	switch d.Run {
	case "install":
		if d.Install == nil {
			return domain.Step{}, fmt.Errorf("step %q: install requires an install table", d.Name)
		}
		st.Ops = []domain.Operation{{Install: mapInstall(d.Install)}}
	case "shell":
		for _, cmd := range d.Commands {
			st.Ops = append(st.Ops, domain.Operation{Raw: cmd})
		}
		if len(d.Verify) > 0 {
			st.Ops = append(st.Ops, domain.Operation{Verify: mapVerify(d.Verify)})
		}
	case "verify":
		st.Ops = []domain.Operation{{Verify: mapVerify(d.Checks)}}
	default:
		return domain.Step{}, fmt.Errorf("step %q: %w: %q", d.Name, domain.ErrUnknownStepKind, d.Run)
	}

	return st, nil
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
	if err := validateOpsApt(ops, c.Name); err != nil {
		return domain.Step{}, err
	}
	st.Ops = ops
	return st, nil
}

// validateOpsApt rejects invalid apt install configurations in a step: a
// package appearing in both build and runtime, a conditional entry whose
// category is not "build" or "runtime", or a conditional package that is not
// listed in its category.
func validateOpsApt(ops []domain.Operation, name string) error {
	for _, op := range ops {
		if op.Install == nil || op.Install.Apt == nil {
			continue
		}
		apt := op.Install.Apt
		build := map[string]bool{}
		for _, p := range apt.Build {
			build[p] = true
		}
		runtime := map[string]bool{}
		for _, p := range apt.Runtime {
			runtime[p] = true
		}
		for p := range build {
			if runtime[p] {
				return fmt.Errorf("step %q: apt package %q appears in both build and runtime", name, p)
			}
		}
		for _, c := range apt.Conditional {
			if c.Category != "build" && c.Category != "runtime" {
				return fmt.Errorf("step %q: apt conditional category must be \"build\" or \"runtime\", got %q", name, c.Category)
			}
			allowed := build
			if c.Category == "runtime" {
				allowed = runtime
			}
			for _, p := range c.Packages {
				if !allowed[p] {
					return fmt.Errorf("step %q: apt conditional package %q is not listed in %s", name, p, c.Category)
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
		if d.Apt != nil {
			op.Apt = &domain.AptOp{Action: d.Apt.Action, Packages: d.Apt.Packages}
		}
		if d.Install != nil {
			op.Install = &domain.InstallOp{
				Apt:    mapAptInstall(d.Install.Apt),
				Source: mapSourceInstall(d.Install.Source),
				Binary: mapBinaryInstall(d.Install.Binary),
			}
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

func mapInstall(d *installOpDTO) *domain.InstallOp {
	if d == nil {
		return nil
	}
	return &domain.InstallOp{
		Apt:    mapAptInstall(d.Apt),
		Source: mapSourceInstall(d.Source),
		Binary: mapBinaryInstall(d.Binary),
	}
}

func mapAptInstall(d *aptInstallDTO) *domain.AptInstall {
	if d == nil {
		return nil
	}
	ai := &domain.AptInstall{Build: d.Build, Runtime: d.Runtime}
	for _, c := range d.Conditional {
		ai.Conditional = append(ai.Conditional, domain.ConditionalApt{
			Category: c.Category,
			When: domain.VersionCondition{Var: c.When.Var, Gte: c.When.Gte, Lte: c.When.Lte, Gt: c.When.Gt, Lt: c.When.Lt, Eq: c.When.Eq},
			Packages: c.Packages,
		})
	}
	return ai
}

func mapSourceInstall(d *sourceInstallDTO) *domain.SourceInstall {
	if d == nil {
		return nil
	}
	return &domain.SourceInstall{
		Type: d.Type, Source: d.Source, Ref: d.Ref, Strategy: d.Strategy,
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
	bi := &domain.BinaryInstall{Source: d.Source}
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
