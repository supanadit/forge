package disk

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/supanadit/forge/domain"
)

func writeManifest(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
	return p
}

func TestLoad_NotFound(t *testing.T) {
	r := NewManifestRepository()
	_, err := r.Load(context.Background(), "/nonexistent/forge.toml")
	assert.ErrorIs(t, err, domain.ErrManifestNotFound)
}

func TestLoad_BasicManifest(t *testing.T) {
	dir := t.TempDir()
	path := writeManifest(t, dir, "forge.toml", `
[project]
name = "demo"
description = "test project"

[vars]
VERSION = "1.0"

[[steps]]
name = "hello"
run = "shell"
commands = ["echo hi"]
`)
	r := NewManifestRepository()
	m, err := r.Load(context.Background(), path)
	require.NoError(t, err)
	assert.Equal(t, "demo", m.Project.Name)
	assert.Equal(t, "1.0", m.Vars["VERSION"])
	require.Len(t, m.Steps, 1)
	assert.Equal(t, domain.StepKindShell, m.Steps[0].Kind)
	assert.Equal(t, []string{"echo hi"}, m.Steps[0].Shell.Commands)
}

func TestLoad_SourceStep(t *testing.T) {
	dir := t.TempDir()
	path := writeManifest(t, dir, "forge.toml", `
[project]
name = "pg"

[[steps]]
name = "build-pg"
run = "source"
fetch = { type = "archive", url = "https://x/postgresql.tar.gz" }
build = { strategy = "configure", prefix = "/usr/local/pgsql", flags = ["--with-openssl"] }
install = true
verify = [{ file = "/usr/local/pgsql/bin/postgres" }]
`)
	r := NewManifestRepository()
	m, err := r.Load(context.Background(), path)
	require.NoError(t, err)
	require.Len(t, m.Steps, 1)
	src := m.Steps[0].Source
	require.NotNil(t, src)
	require.NotNil(t, src.Fetch)
	assert.Equal(t, domain.FetchTypeArchive, src.Fetch.Type)
	assert.Equal(t, "https://x/postgresql.tar.gz", src.Fetch.Archive.URL)
	require.NotNil(t, src.Build)
	assert.Equal(t, domain.BuildStrategyConfigure, src.Build.Strategy)
	assert.True(t, src.Install)
	require.Len(t, src.Verify, 1)
	assert.Equal(t, "/usr/local/pgsql/bin/postgres", src.Verify[0].File)
}

func TestLoad_AptStep(t *testing.T) {
	dir := t.TempDir()
	path := writeManifest(t, dir, "forge.toml", `
[[steps]]
name = "deps"
run = "apt"
action = "install"
packages = ["curl", "git"]

[[steps]]
name = "cond"
run = "apt"
action = "install"
packages = ["base"]
packages_conditional = [{ condition = "x", packages = ["bison", "flex"] }]
`)
	r := NewManifestRepository()
	m, err := r.Load(context.Background(), path)
	require.NoError(t, err)
	require.Len(t, m.Steps, 2)
	apt := m.Steps[0].Apt
	assert.Equal(t, "install", apt.Action)
	assert.Equal(t, []string{"curl", "git"}, apt.Packages)
	cond := m.Steps[1].Apt.PackagesConditional
	require.Len(t, cond, 1)
	assert.Equal(t, "x", cond[0].Condition)
	assert.Equal(t, []string{"bison", "flex"}, cond[0].Packages)
}

func TestLoad_BinaryStep(t *testing.T) {
	dir := t.TempDir()
	path := writeManifest(t, dir, "forge.toml", `
[[steps]]
name = "metrics"
run = "binary"
fetch = { type = "archive", url = "https://x/pgmetrics.tar.gz" }
install = { copy = [{ from = "pgmetrics", to = "/usr/local/bin/pgmetrics", mode = "0755" }] }
`)
	r := NewManifestRepository()
	m, err := r.Load(context.Background(), path)
	require.NoError(t, err)
	require.Len(t, m.Steps, 1)
	bin := m.Steps[0].Binary
	require.NotNil(t, bin)
	require.NotNil(t, bin.Fetch)
	assert.Equal(t, domain.FetchTypeArchive, bin.Fetch.Type)
	require.NotNil(t, bin.Install)
	require.Len(t, bin.Install.Copy, 1)
	assert.Equal(t, "/usr/local/bin/pgmetrics", bin.Install.Copy[0].To)
	assert.Equal(t, "0755", bin.Install.Copy[0].Mode)
}

func TestLoad_UnknownKind(t *testing.T) {
	dir := t.TempDir()
	path := writeManifest(t, dir, "forge.toml", `
[[steps]]
name = "weird"
run = "frobnicate"
`)
	r := NewManifestRepository()
	_, err := r.Load(context.Background(), path)
	assert.ErrorIs(t, err, domain.ErrInvalidManifest)
	assert.Contains(t, err.Error(), "unknown step kind")
}

func TestLoad_InlineIncludeSplice(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, "shared/deps.toml", `
[[steps]]
name = "shared-dep"
run = "apt"
action = "install"
packages = ["curl"]
`)
	path := writeManifest(t, dir, "forge.toml", `
[project]
name = "main"

[[includes]]
path = "shared/deps.toml"

[[steps]]
name = "own"
run = "shell"
commands = ["echo own"]
`)
	r := NewManifestRepository()
	m, err := r.Load(context.Background(), path)
	require.NoError(t, err)
	require.Len(t, m.Steps, 2)
	assert.Equal(t, "shared-dep", m.Steps[0].Name)
	assert.Equal(t, "own", m.Steps[1].Name)
}

func TestLoad_NamedGroupReference(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, "shared/locale.toml", `
[[steps]]
name = "locale-gen"
run = "shell"
commands = ["locale-gen"]
`)
	path := writeManifest(t, dir, "forge.toml", `
[[includes]]
path = "shared/locale.toml"
as = "locale"

[[steps]]
name = "before"
run = "shell"
commands = ["echo before"]

[[steps]]
use = "locale"

[[steps]]
name = "after"
run = "shell"
commands = ["echo after"]
`)
	r := NewManifestRepository()
	m, err := r.Load(context.Background(), path)
	require.NoError(t, err)
	require.Len(t, m.Steps, 3)
	assert.Equal(t, []string{"before", "locale-gen", "after"}, []string{
		m.Steps[0].Name, m.Steps[1].Name, m.Steps[2].Name,
	})
}

func TestLoad_UnknownGroupReference(t *testing.T) {
	dir := t.TempDir()
	path := writeManifest(t, dir, "forge.toml", `
[[steps]]
use = "ghost"
`)
	r := NewManifestRepository()
	_, err := r.Load(context.Background(), path)
	assert.ErrorIs(t, err, domain.ErrInvalidManifest)
	assert.Contains(t, err.Error(), "unknown include group")
}

func TestLoad_NestedIncludes(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, "shared/base.toml", `
[[steps]]
name = "base"
run = "shell"
commands = ["echo base"]
`)
	writeManifest(t, dir, "shared/mid.toml", `
[[includes]]
path = "base.toml"

[[steps]]
name = "mid"
run = "shell"
commands = ["echo mid"]
`)
	path := writeManifest(t, dir, "forge.toml", `
[[includes]]
path = "shared/mid.toml"
`)
	r := NewManifestRepository()
	m, err := r.Load(context.Background(), path)
	require.NoError(t, err)
	require.Len(t, m.Steps, 2)
	assert.Equal(t, []string{"base", "mid"}, []string{m.Steps[0].Name, m.Steps[1].Name})
}

func TestLoad_InvalidTOML(t *testing.T) {
	dir := t.TempDir()
	path := writeManifest(t, dir, "forge.toml", `[project`)
	r := NewManifestRepository()
	_, err := r.Load(context.Background(), path)
	assert.ErrorIs(t, err, domain.ErrInvalidManifest)
}
