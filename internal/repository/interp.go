// Package repository holds driver-agnostic helpers shared by forge's
// infrastructure drivers. Files here are not modules; they are utilities
// used by internal/repository/<driver> implementations.
package repository

import "strings"

// Lookup returns the value bound to a variable name, and whether it is set.
type Lookup func(name string) (value string, ok bool)

// envLookup wraps a map as a Lookup.
func envLookup(vars map[string]string) Lookup {
	return func(name string) (string, bool) {
		v, ok := vars[name]
		return v, ok
	}
}

// Replace expands ${VAR} and ${VAR:-default} placeholders in s using the
// given lookup. A placeholder with no match and no default is left verbatim.
// The default may itself contain placeholders, which are recursively expanded
// (bounded to avoid infinite recursion).
func Replace(s string, lookup Lookup) string {
	return replace(s, lookup, 0)
}

func replace(s string, lookup Lookup, depth int) string {
	if depth > 32 || !strings.Contains(s, "${") {
		return s
	}
	var b strings.Builder
	for {
		start := strings.Index(s, "${")
		if start < 0 {
			b.WriteString(s)
			break
		}
		b.WriteString(s[:start])
		rest := s[start+2:]

		// Find the matching close brace, tracking nested ${...} depth.
		end := -1
		nest := 0
		for i := 0; i < len(rest); i++ {
			if rest[i] == '{' && i > 0 && rest[i-1] == '$' {
				nest++
				continue
			}
			if rest[i] == '}' {
				if nest == 0 {
					end = i
					break
				}
				nest--
			}
		}
		if end < 0 {
			b.WriteString(s[start:])
			break
		}
		expr := rest[:end]
		b.WriteString(expandExpr(expr, lookup, depth))
		s = rest[end+1:]
	}
	return b.String()
}

func expandExpr(expr string, lookup Lookup, depth int) string {
	name := expr
	hasDefault := false
	var def string
	if idx := strings.Index(expr, ":-"); idx >= 0 {
		name = expr[:idx]
		hasDefault = true
		def = expr[idx+2:]
	}
	if v, ok := lookup(name); ok {
		return v
	}
	if hasDefault {
		return replace(def, lookup, depth+1)
	}
	return "${" + expr + "}"
}

// MergeVars merges override onto base and returns the result. Values in
// override win. Neither input is mutated.
func MergeVars(base, override map[string]string) map[string]string {
	merged := make(map[string]string, len(base)+len(override))
	for k, v := range base {
		merged[k] = v
	}
	for k, v := range override {
		merged[k] = v
	}
	return merged
}

// EnvToVars converts a slice of KEY=VALUE strings into a map.
func EnvToVars(env []string) map[string]string {
	out := make(map[string]string, len(env))
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok {
			out[k] = v
		}
	}
	return out
}
