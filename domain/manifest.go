// Package domain holds the pure data model and domain errors for forge.
// It contains no tags, no interfaces, and no external dependencies.
package domain

// Manifest is the root build definition loaded from a forge.toml file.
type Manifest struct {
	// Project is the metadata about the project being built.
	Project Project
	// Vars are the default build variables (key -> value). They are
	// overridden by environment variables and CLI -var flags at build time.
	Vars map[string]string
	// Steps are the ordered build steps, already resolved from includes.
	Steps []Step
	// Includes are the resolved include sources (file paths) that contributed
	// steps to this manifest. Used for diagnostics.
	Includes []string
}

// Project describes the project being built.
type Project struct {
	Name        string
	Description string
}
