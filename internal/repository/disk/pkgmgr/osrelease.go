package pkgmgr

import (
	"os"
	"strings"
)

// osReleaseInfo reads the ID and ID_LIKE fields from /etc/os-release. Any
// read error returns empty values so detection can fall back to binary lookup.
func osReleaseInfo() (id, idLike string) {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return "", ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		value = strings.Trim(value, `"'`)
		switch key {
		case "ID":
			id = value
		case "ID_LIKE":
			idLike = value
		}
	}
	return id, idLike
}

// parseOsRelease parses os-release content, returning the ID and ID_LIKE
// values. Exported for tests with synthetic content.
func parseOsRelease(content string) (id, idLike string) {
	for _, line := range strings.Split(content, "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		value = strings.Trim(value, `"'`)
		switch key {
		case "ID":
			id = value
		case "ID_LIKE":
			idLike = value
		}
	}
	return id, idLike
}