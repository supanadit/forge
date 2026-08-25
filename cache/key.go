package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strconv"
	"strings"

	"github.com/supanadit/forge/domain"
)

// ComputeKey returns a deterministic content hash identifying a step's inputs:
// its own configuration, the resolved vars it uses, and the cache keys of all
// its dependencies (so changing a dependency transitively invalidates the
// step). The key is stable across runs with identical inputs, enabling the
// build cache to restore the step's output instead of re-executing it.
func ComputeKey(step domain.Step, vars map[string]string, deps map[string]string) string {
	h := sha256.New()

	write := func(v ...string) {
		for _, s := range v {
			h.Write([]byte(s))
			h.Write([]byte{0})
		}
	}

	write("name", step.Name)

	// Serialize the step's configuration deterministically.
	write(configString(step))

	// Vars: only those referenced by the step's config, sorted for stability.
	for _, name := range sortedVars(configString(step)) {
		if v, ok := vars[name]; ok {
			write("var", name, v)
		}
	}

	// Dependencies' cache keys, sorted for determinism.
	depKeys := make([]string, 0, len(deps))
	for name, key := range deps {
		depKeys = append(depKeys, name+"="+key)
	}
	sort.Strings(depKeys)
	for _, dk := range depKeys {
		write("dep", dk)
	}

	return hex.EncodeToString(h.Sum(nil))
}

// configString returns a stable, sortable representation of a step's config.
func configString(step domain.Step) string {
	var parts []string
	parts = append(parts, "ops="+opsString(step.Ops))
	sort.Strings(parts)
	return strings.Join(parts, "|")
}

func buildString(b *domain.BuildSpec) string {
	if b == nil {
		return ""
	}
	var parts []string
	parts = append(parts,
		"strategy="+string(b.Strategy),
		"prefix="+b.Prefix,
		"flags="+strings.Join(b.Flags, ","),
		"env="+mapString(b.Env),
		"make_flags="+strings.Join(b.MakeFlags, ","),
		"jobs="+strconv.Itoa(b.Jobs),
		"install_target="+b.InstallTarget,
	)
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

func binaryInstallString(b *domain.BinaryInstall) string {
	if b == nil {
		return ""
	}
	var parts []string
	for _, c := range b.Copy {
		parts = append(parts, c.From+"->"+c.To+":"+c.Mode)
	}
	sort.Strings(parts)
	return strings.Join(parts, ";")
}

func mapString(m map[string]string) string {
	if len(m) == 0 {
		return ""
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(m[k])
	}
	return b.String()
}

func verifyString(v []domain.VerifyCheck) string {
	if len(v) == 0 {
		return ""
	}
	var parts []string
	for _, c := range v {
		parts = append(parts, c.File)
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

// opsString returns a stable representation of an operation list.
func opsString(ops []domain.Operation) string {
	if len(ops) == 0 {
		return ""
	}
	var parts []string
	for _, op := range ops {
		parts = append(parts, opString(op))
	}
	return strings.Join(parts, ";")
}

func opString(op domain.Operation) string {
	var parts []string
	if op.Raw != "" {
		parts = append(parts, "raw="+op.Raw)
	}
	if op.User != nil {
		parts = append(parts, "user="+op.User.Name)
	}
	for _, m := range op.Mkdir {
		parts = append(parts, "mkdir="+m.Path+":"+m.Mode+":"+m.Owner)
	}
	for _, c := range op.Chown {
		parts = append(parts, "chown="+c.Path+":"+c.Owner+":"+c.Group)
	}
	for _, c := range op.Chmod {
		parts = append(parts, "chmod="+c.Path+":"+c.Mode)
	}
	for _, c := range op.Copy {
		parts = append(parts, "copy="+c.From+"->"+c.To+":"+c.Mode)
	}
	for _, t := range op.Touch {
		parts = append(parts, "touch="+t)
	}
	if op.Apt != nil {
		parts = append(parts, "apt="+op.Apt.Action+":"+strings.Join(op.Apt.Packages, ","))
	}
	if op.Install != nil {
		parts = append(parts, "install="+installString(op.Install))
	}
	if len(op.Verify) > 0 {
		parts = append(parts, "verify="+verifyString(op.Verify))
	}
	if op.Generate != nil {
		parts = append(parts, "generate="+generateString(op.Generate))
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

func generateString(g *domain.GenerateOp) string {
	if g == nil {
		return ""
	}
	var parts []string
	parts = append(parts,
		"tool="+g.Tool,
		"input="+g.Input,
		"out="+g.Out,
		"flags="+strings.Join(g.Flags, ","),
	)
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

func installString(inst *domain.InstallOp) string {
	if inst == nil {
		return ""
	}
	var parts []string
	if inst.Apt != nil {
		parts = append(parts, "apt_build="+strings.Join(inst.Apt.Build, ","))
		parts = append(parts, "apt_runtime="+strings.Join(inst.Apt.Runtime, ","))
		for _, c := range inst.Apt.Conditional {
			w := c.When
			parts = append(parts, "apt_cond="+w.Var+":"+w.Gte+"/"+w.Lte+"/"+w.Gt+"/"+w.Lt+"/"+w.Eq+":"+strings.Join(c.Packages, ","))
		}
	}
	if inst.Source != nil {
		src := inst.Source
		parts = append(parts,
			"type="+src.Type,
			"source="+src.Source,
			"ref="+src.Ref,
			"strategy="+src.Strategy,
			"flags="+strings.Join(src.Flags, ","),
			"prefix="+src.Prefix,
			"jobs="+strconv.Itoa(src.Jobs),
			"install_target="+src.InstallTarget,
			"env="+mapString(src.Env),
			"verify="+verifyString(src.Verify),
			"before="+opsString(src.Before),
			"after="+opsString(src.After),
		)
	}
	if inst.Binary != nil {
		bin := inst.Binary
		parts = append(parts,
			"binary_source="+bin.Source,
			"copy="+binaryInstallString(bin),
		)
	}
	sort.Strings(parts)
	return strings.Join(parts, "|")
}

// sortedVars returns the var names referenced as ${NAME} (or ${NAME:-def})
// in a config string, de-duplicated and sorted.
func sortedVars(s string) []string {
	seen := map[string]bool{}
	var out []string
	// Scan for ${...} and extract the variable name (before any :- default or
	// shell modifier).
	rest := s
	for {
		idx := strings.Index(rest, "${")
		if idx < 0 {
			break
		}
		rest = rest[idx+2:]
		end := strings.Index(rest, "}")
		if end < 0 {
			break
		}
		expr := rest[:end]
		name := expr
		if i := strings.IndexAny(expr, ":-%:/+?"); i >= 0 {
			name = expr[:i]
		}
		name = strings.TrimSpace(name)
		if name != "" && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
		rest = rest[end+1:]
	}
	sort.Strings(out)
	return out
}
