package repository

import "testing"

func TestReplace_Simple(t *testing.T) {
	vars := map[string]string{"VERSION": "13.5"}
	got := Replace("v${VERSION}.tar.gz", envLookup(vars))
	if got != "v13.5.tar.gz" {
		t.Fatalf("got %q", got)
	}
}

func TestReplace_MissingNoDefault(t *testing.T) {
	got := Replace("${NOPE}", envLookup(map[string]string{}))
	if got != "${NOPE}" {
		t.Fatalf("got %q", got)
	}
}

func TestReplace_Default(t *testing.T) {
	got := Replace("${PORT:-5432}", envLookup(map[string]string{}))
	if got != "5432" {
		t.Fatalf("got %q", got)
	}
}

func TestReplace_DefaultWithExistingValue(t *testing.T) {
	got := Replace("${PORT:-5432}", envLookup(map[string]string{"PORT": "6432"}))
	if got != "6432" {
		t.Fatalf("got %q", got)
	}
}

func TestReplace_Multiple(t *testing.T) {
	vars := map[string]string{"A": "1", "B": "2"}
	got := Replace("${A}-${B}-${A}", envLookup(vars))
	if got != "1-2-1" {
		t.Fatalf("got %q", got)
	}
}

func TestReplace_NestedDefault(t *testing.T) {
	vars := map[string]string{"A": "x"}
	got := Replace("${NOPE:-${A}}", envLookup(vars))
	if got != "x" {
		t.Fatalf("got %q", got)
	}
}

func TestReplace_NoPlaceholders(t *testing.T) {
	got := Replace("plain string", envLookup(map[string]string{}))
	if got != "plain string" {
		t.Fatalf("got %q", got)
	}
}

func TestReplace_SpecExpressionLeftVerbatim(t *testing.T) {
	vars := map[string]string{"POSTGRESQL_VERSION": "13.5"}
	// %%.* is bash-specific parameter expansion; forge leaves it verbatim
	// so the shell/condition evaluator resolves it at execution time.
	got := Replace("${POSTGRESQL_VERSION%%.*}", envLookup(vars))
	if got != "${POSTGRESQL_VERSION%%.*}" {
		t.Fatalf("got %q", got)
	}
}

func TestMergeVars(t *testing.T) {
	base := map[string]string{"a": "1", "b": "2"}
	override := map[string]string{"b": "3", "c": "4"}
	merged := MergeVars(base, override)
	if merged["a"] != "1" || merged["b"] != "3" || merged["c"] != "4" {
		t.Fatalf("got %v", merged)
	}
	if base["b"] != "2" {
		t.Fatal("base mutated")
	}
}

func TestEnvToVars(t *testing.T) {
	got := EnvToVars([]string{"A=1", "B=two", "noequals"})
	if got["A"] != "1" || got["B"] != "two" {
		t.Fatalf("got %v", got)
	}
	if _, ok := got["noequals"]; ok {
		t.Fatal("noequals should be skipped")
	}
}
