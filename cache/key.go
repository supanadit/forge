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
	write("kind", string(step.Kind))

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
	switch step.Kind {
	case domain.StepKindApt:
		if step.Apt != nil {
			parts = append(parts,
				"action="+step.Apt.Action,
				"packages="+strings.Join(step.Apt.Packages, ","),
			)
			for _, cp := range step.Apt.PackagesConditional {
				parts = append(parts, "cond="+cp.Condition+":"+strings.Join(cp.Packages, ","))
			}
		}
	case domain.StepKindSource:
		if step.Source != nil {
			parts = append(parts,
				"fetch="+fetchString(step.Source.Fetch),
				"build="+buildString(step.Source.Build),
				"install="+strconv.FormatBool(step.Source.Install),
				"from="+step.Source.From,
				"dir="+step.Source.Dir,
				"env="+mapString(step.Source.Env),
				"verify="+verifyString(step.Source.Verify),
				"cache_verify="+verifyString(step.Source.CacheVerify),
			)
		}
	case domain.StepKindBinary:
		if step.Binary != nil {
			parts = append(parts,
				"fetch="+fetchString(step.Binary.Fetch),
				"install="+binaryInstallString(step.Binary.Install),
				"verify="+verifyString(step.Binary.Verify),
				"cache_verify="+verifyString(step.Binary.CacheVerify),
			)
		}
	case domain.StepKindShell:
		if step.Shell != nil {
			parts = append(parts,
				"commands="+strings.Join(step.Shell.Commands, "\n"),
				"env="+mapString(step.Shell.Env),
				"dir="+step.Shell.Dir,
				"verify="+verifyString(step.Shell.Verify),
				"cache_verify="+verifyString(step.Shell.CacheVerify),
			)
		}
	case domain.StepKindVerify:
		if step.Verify != nil {
			parts = append(parts, "checks="+verifyString(step.Verify.Checks))
		}
	}
	sort.Strings(parts)
	return strings.Join(parts, "|")
}

func fetchString(f *domain.FetchSpec) string {
	if f == nil {
		return ""
	}
	var parts []string
	parts = append(parts, "type="+string(f.Type))
	if f.Archive != nil {
		parts = append(parts,
			"url="+f.Archive.URL,
			"checksum_type="+f.Archive.ChecksumType,
			"checksum="+f.Archive.Checksum,
			"dest="+f.Archive.Dest,
		)
	}
	if f.Git != nil {
		parts = append(parts,
			"url="+f.Git.URL,
			"ref="+f.Git.Ref,
			"depth="+strconv.Itoa(f.Git.Depth),
			"dest="+f.Git.Dest,
		)
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
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
