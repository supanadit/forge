package pkgmgr

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseOsRelease(t *testing.T) {
	id, idLike := parseOsRelease(`
PRETTY_NAME="Debian GNU/Linux 12 (bookworm)"
NAME="Debian GNU/Linux"
ID=debian
HOME_URL="https://www.debian.org/"
`)
	assert.Equal(t, "debian", id)
	assert.Equal(t, "", idLike)
}

func TestParseOsReleaseQuoted(t *testing.T) {
	id, idLike := parseOsRelease(`
NAME="Rocky Linux"
ID="rocky"
ID_LIKE="rhel fedora"
`)
	assert.Equal(t, "rocky", id)
	assert.Equal(t, "rhel fedora", idLike)
}

func TestFromOSRelease(t *testing.T) {
	cases := []struct {
		id, idLike string
		want       string
	}{
		{"debian", "", "apt"},
		{"ubuntu", "debian", "apt"},
		{"alpine", "", "apk"},
		{"fedora", "", "dnf"},
		{"rhel", "", "dnf"},
		{"rocky", "rhel fedora", "dnf"},
		{"ol", "", "dnf"},
		{"amzn", "", "yum"},
		{"suse", "", ""},
	}
	for _, c := range cases {
		m, ok := fromOSRelease(c.id, c.idLike)
		if c.want == "" {
			assert.False(t, ok, "id=%s idLike=%s", c.id, c.idLike)
			continue
		}
		require.True(t, ok, "id=%s idLike=%s", c.id, c.idLike)
		assert.Equal(t, c.want, m.ID())
	}
}

func TestFromName(t *testing.T) {
	assert.Equal(t, "apt", mustFromName(t, "apt").ID())
	assert.Equal(t, "dnf", mustFromName(t, "dnf").ID())
	assert.Equal(t, "yum", mustFromName(t, "yum").ID())
	assert.Equal(t, "apk", mustFromName(t, "apk").ID())
	_, err := fromName("brew")
	require.Error(t, err)
}

func mustFromName(t *testing.T, name string) Manager {
	t.Helper()
	m, err := fromName(name)
	require.NoError(t, err)
	return m
}

func TestResolveNames(t *testing.T) {
	// Known meta-package maps per manager.
	assert.Equal(t, []string{"build-essential"}, ResolveNames("apt", []string{"build-essential"}))
	assert.Equal(t, []string{"gcc", "gcc-c++", "make"}, ResolveNames("dnf", []string{"build-essential"}))
	assert.Equal(t, []string{"build-base"}, ResolveNames("apk", []string{"build-essential"}))

	// -dev/-devel suffix convention.
	assert.Equal(t, []string{"libssl-dev"}, ResolveNames("apt", []string{"libssl-dev"}))
	assert.Equal(t, []string{"libssl-devel"}, ResolveNames("dnf", []string{"libssl-dev"}))
	assert.Equal(t, []string{"libssl-devel"}, ResolveNames("yum", []string{"libssl-dev"}))
	assert.Equal(t, []string{"libssl-dev"}, ResolveNames("apk", []string{"libssl-dev"}))

	// Unknown names pass through.
	assert.Equal(t, []string{"curl", "git"}, ResolveNames("apt", []string{"curl", "git"}))
	assert.Equal(t, []string{"curl", "git"}, ResolveNames("dnf", []string{"curl", "git"}))
}

func TestVirtualGroupDeterministic(t *testing.T) {
	a := VirtualGroup([]string{"bison", "flex"})
	b := VirtualGroup([]string{"bison", "flex"})
	assert.Equal(t, a, b, "same set → same group")
	assert.NotEqual(t, a, VirtualGroup([]string{"flex", "bison", "make"}))
	assert.Equal(t, a, VirtualGroup([]string{"flex", "bison"}), "order must not matter")
}

func TestFilterInstalled(t *testing.T) {
	installed := map[string]bool{
		"curl":  true,
		"git":   true,
		"apt":   true,
		"nano":  true,
	}
	checker := func(pkg string) bool { return installed[pkg] }

	// All installed
	result := filterInstalled([]string{"curl", "git", "apt"}, checker, false)
	assert.Equal(t, []string{"curl", "git", "apt"}, result)

	// Some installed
	result = filterInstalled([]string{"curl", "vim", "git", "emacs"}, checker, false)
	assert.Equal(t, []string{"curl", "git"}, result)

	// None installed
	result = filterInstalled([]string{"vim", "emacs"}, checker, false)
	assert.Empty(t, result)

	// Empty input
	result = filterInstalled(nil, checker, false)
	assert.Nil(t, result)
	result = filterInstalled([]string{}, checker, false)
	assert.Nil(t, result)
}

func TestCheckerNilSafeOnFailure(t *testing.T) {
	// Detection on the current machine may or may not succeed; the checker
	// must never panic and always answer something for an unknown package.
	c := NewChecker()
	_ = c.Installed("definitely-not-a-real-pkg-forge-test")
}