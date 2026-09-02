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

[[components]]
name = "hello"
ops = [{ raw = "echo hi" }]
`)
	r := NewManifestRepository()
	m, err := r.Load(context.Background(), path)
	require.NoError(t, err)
	assert.Equal(t, "demo", m.Project.Name)
	assert.Equal(t, "1.0", m.Vars["VERSION"])
	require.Len(t, m.Steps, 1)
	assert.Equal(t, "hello", m.Steps[0].Name)
	assert.Equal(t, "echo hi", m.Steps[0].Ops[0].Raw)
}

func TestLoad_PackagesArrayForm(t *testing.T) {
	dir := t.TempDir()
	path := writeManifest(t, dir, "forge.toml", `
[[components]]
name = "deps"
ops = [{ packages = ["curl", "git"] }]
`)
	r := NewManifestRepository()
	m, err := r.Load(context.Background(), path)
	require.NoError(t, err)
	require.Len(t, m.Steps, 1)
	p := m.Steps[0].Ops[0].Packages
	require.NotNil(t, p)
	assert.Equal(t, []string{"curl", "git"}, p.Runtime)
	assert.Empty(t, p.Build)
	assert.Empty(t, p.Remove)
}

func TestLoad_PackagesTableForm(t *testing.T) {
	dir := t.TempDir()
	path := writeManifest(t, dir, "forge.toml", `
[[components]]
name = "deps"
ops = [{ packages = { build = ["base", "bison", "flex"],
                      runtime = ["curl"],
                      remove = ["vim-tiny"],
                      conditional = [{ category = "build", when = { var = "PG", gte = "17" }, packages = ["bison", "flex"] }] } }]
`)
	r := NewManifestRepository()
	m, err := r.Load(context.Background(), path)
	require.NoError(t, err)
	require.Len(t, m.Steps, 1)
	p := m.Steps[0].Ops[0].Packages
	require.NotNil(t, p)
	assert.Equal(t, []string{"base", "bison", "flex"}, p.Build)
	assert.Equal(t, []string{"curl"}, p.Runtime)
	assert.Equal(t, []string{"vim-tiny"}, p.Remove)
	require.Len(t, p.Conditional, 1)
	assert.Equal(t, "build", p.Conditional[0].Category)
	assert.Equal(t, "PG", p.Conditional[0].When.Var)
	assert.Equal(t, "17", p.Conditional[0].When.Gte)
	assert.Equal(t, []string{"bison", "flex"}, p.Conditional[0].Packages)
}

func TestLoad_SourceInstallComponent(t *testing.T) {
	dir := t.TempDir()
	path := writeManifest(t, dir, "forge.toml", `
[project]
name = "pg"

[[components]]
name = "build-pg"
ops = [
  { source_install = { type = "archive", url = "https://x/postgresql.tar.gz", strategy = "configure", prefix = "/usr/local/pgsql", flags = ["--with-openssl"], install_target = "altinstall", verify = [{ file = "/usr/local/pgsql/bin/postgres" }] } },
]
`)
	r := NewManifestRepository()
	m, err := r.Load(context.Background(), path)
	require.NoError(t, err)
	require.Len(t, m.Steps, 1)
	require.Len(t, m.Steps[0].Ops, 1)
	src := m.Steps[0].Ops[0].SourceInstall
	require.NotNil(t, src)
	assert.Equal(t, "archive", src.Type)
	assert.Equal(t, "https://x/postgresql.tar.gz", src.URL)
	assert.Equal(t, "configure", src.Strategy)
	assert.Equal(t, "/usr/local/pgsql", src.Prefix)
	assert.Equal(t, "altinstall", src.InstallTarget)
	require.Len(t, src.Verify, 1)
	assert.Equal(t, "/usr/local/pgsql/bin/postgres", src.Verify[0].File)
}

func TestLoad_BinaryInstallComponent(t *testing.T) {
	dir := t.TempDir()
	path := writeManifest(t, dir, "forge.toml", `
[[components]]
name = "metrics"
ops = [{ binary_install = { url = "https://x/pgmetrics.tar.gz", copy = [{ from = "pgmetrics", to = "/usr/local/bin/pgmetrics", mode = "0755" }] } }]
`)
	r := NewManifestRepository()
	m, err := r.Load(context.Background(), path)
	require.NoError(t, err)
	require.Len(t, m.Steps, 1)
	bin := m.Steps[0].Ops[0].BinaryInstall
	require.NotNil(t, bin)
	assert.Equal(t, "https://x/pgmetrics.tar.gz", bin.URL)
	require.Len(t, bin.Copy, 1)
	assert.Equal(t, "/usr/local/bin/pgmetrics", bin.Copy[0].To)
	assert.Equal(t, "0755", bin.Copy[0].Mode)
}

func TestLoad_RejectsLegacyInstallForm(t *testing.T) {
	dir := t.TempDir()
	path := writeManifest(t, dir, "forge.toml", `
[[components]]
name = "old"
ops = [{ install = { source = { type = "archive", source = "https://x/t.tgz" } } }]
`)
	r := NewManifestRepository()
	_, err := r.Load(context.Background(), path)
	assert.ErrorIs(t, err, domain.ErrInvalidManifest)
}

func TestLoad_RejectsLegacyAptOp(t *testing.T) {
	dir := t.TempDir()
	path := writeManifest(t, dir, "forge.toml", `
[[components]]
name = "old"
ops = [{ apt = { action = "install", packages = ["curl"] } }]
`)
	r := NewManifestRepository()
	_, err := r.Load(context.Background(), path)
	assert.ErrorIs(t, err, domain.ErrInvalidManifest)
}

func TestLoad_RejectsLegacyStepsForm(t *testing.T) {
	dir := t.TempDir()
	path := writeManifest(t, dir, "forge.toml", `
[[steps]]
name = "hello"
run = "shell"
commands = ["echo hi"]
`)
	r := NewManifestRepository()
	_, err := r.Load(context.Background(), path)
	assert.ErrorIs(t, err, domain.ErrInvalidManifest)
}

func TestLoad_PackagesOverlapRejected(t *testing.T) {
	dir := t.TempDir()
	path := writeManifest(t, dir, "forge.toml", `
[[components]]
name = "bad"
ops = [{ packages = { build = ["curl"], runtime = ["curl"] } }]
`)
	r := NewManifestRepository()
	_, err := r.Load(context.Background(), path)
	assert.ErrorIs(t, err, domain.ErrInvalidManifest)
	assert.Contains(t, err.Error(), "both build and runtime")
}

func TestLoad_InlineIncludeSplice(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, "shared/deps.toml", `
[[components]]
name = "shared-dep"
ops = [{ packages = ["curl"] }]
`)
	path := writeManifest(t, dir, "forge.toml", `
[project]
name = "main"

[[includes]]
path = "shared/deps.toml"

[[components]]
name = "own"
ops = [{ raw = "echo own" }]
`)
	r := NewManifestRepository()
	m, err := r.Load(context.Background(), path)
	require.NoError(t, err)
	require.Len(t, m.Steps, 2)
	assert.Equal(t, "shared-dep", m.Steps[0].Name)
	assert.Equal(t, "own", m.Steps[1].Name)
}

func TestLoad_NestedIncludes(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, "shared/base.toml", `
[[components]]
name = "base"
ops = [{ raw = "echo base" }]
`)
	writeManifest(t, dir, "shared/mid.toml", `
[[includes]]
path = "base.toml"

[[components]]
name = "mid"
ops = [{ raw = "echo mid" }]
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