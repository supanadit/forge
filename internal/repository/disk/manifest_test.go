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
	assert.Equal(t, "echo hi", m.Steps[0].Ops[0].Raw)
}

func TestLoad_InstallComponent(t *testing.T) {
	dir := t.TempDir()
	path := writeManifest(t, dir, "forge.toml", `
[project]
name = "pg"

[[components]]
name = "build-pg"
ops = [
  { install = { source = { type = "archive", source = "https://x/postgresql.tar.gz", strategy = "configure", prefix = "/usr/local/pgsql", flags = ["--with-openssl"], install_target = "altinstall", verify = [{ file = "/usr/local/pgsql/bin/postgres" }] } } },
]
`)
	r := NewManifestRepository()
	m, err := r.Load(context.Background(), path)
	require.NoError(t, err)
	require.Len(t, m.Steps, 1)
	require.Len(t, m.Steps[0].Ops, 1)
	inst := m.Steps[0].Ops[0].Install
	require.NotNil(t, inst)
	require.NotNil(t, inst.Source)
	assert.Equal(t, "archive", inst.Source.Type)
	assert.Equal(t, "https://x/postgresql.tar.gz", inst.Source.Source)
	assert.Equal(t, "configure", inst.Source.Strategy)
	assert.Equal(t, "/usr/local/pgsql", inst.Source.Prefix)
	assert.Equal(t, "altinstall", inst.Source.InstallTarget)
	require.Len(t, inst.Source.Verify, 1)
	assert.Equal(t, "/usr/local/pgsql/bin/postgres", inst.Source.Verify[0].File)
}

func TestLoad_AptInstallComponent(t *testing.T) {
	dir := t.TempDir()
	path := writeManifest(t, dir, "forge.toml", `
[[components]]
name = "deps"
ops = [{ install = { apt = { build = ["curl"], runtime = ["git"] } } }]

[[components]]
name = "cond"
ops = [{ install = { apt = { build = ["base", "bison", "flex"], conditional = [{ category = "build", when = { var = "PG", gte = "17" }, packages = ["bison", "flex"] }] } } }]
`)
	r := NewManifestRepository()
	m, err := r.Load(context.Background(), path)
	require.NoError(t, err)
	require.Len(t, m.Steps, 2)
	apt := m.Steps[0].Ops[0].Install.Apt
	require.NotNil(t, apt)
	assert.Equal(t, []string{"curl"}, apt.Build)
	assert.Equal(t, []string{"git"}, apt.Runtime)
	cond := m.Steps[1].Ops[0].Install.Apt.Conditional
	require.Len(t, cond, 1)
	assert.Equal(t, "build", cond[0].Category)
	assert.Equal(t, "PG", cond[0].When.Var)
	assert.Equal(t, "17", cond[0].When.Gte)
	assert.Equal(t, []string{"bison", "flex"}, cond[0].Packages)
}

func TestLoad_BinaryInstallComponent(t *testing.T) {
	dir := t.TempDir()
	path := writeManifest(t, dir, "forge.toml", `
[[components]]
name = "metrics"
ops = [{ install = { binary = { source = "https://x/pgmetrics.tar.gz", copy = [{ from = "pgmetrics", to = "/usr/local/bin/pgmetrics", mode = "0755" }] } } }]
`)
	r := NewManifestRepository()
	m, err := r.Load(context.Background(), path)
	require.NoError(t, err)
	require.Len(t, m.Steps, 1)
	bin := m.Steps[0].Ops[0].Install.Binary
	require.NotNil(t, bin)
	assert.Equal(t, "https://x/pgmetrics.tar.gz", bin.Source)
	require.Len(t, bin.Copy, 1)
	assert.Equal(t, "/usr/local/bin/pgmetrics", bin.Copy[0].To)
	assert.Equal(t, "0755", bin.Copy[0].Mode)
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
run = "install"
install = { apt = { packages = ["curl"] } }
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
