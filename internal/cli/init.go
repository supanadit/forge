package cli

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

const stubManifest = `[project]
name = "my-project"
description = ""

[vars]

[[components]]
name = "base"
ops = [
  { packages = ["curl", "git"] },
  { raw = "echo 'hello from forge'" },
]
`

// InitHandler scaffolds a new forge.toml manifest.
// It is pure delivery — writing a template file, no use case involved.
type InitHandler struct{}

// NewInitHandler constructs the handler and registers the init subcommand.
func NewInitHandler(root *cobra.Command) {
	root.AddCommand(&cobra.Command{
		Use:   "init [dir]",
		Short: "Scaffold a new forge.toml manifest",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) > 0 {
				dir = args[0]
			}
			path := filepath.Join(dir, "forge.toml")
			if _, err := os.Stat(path); err == nil {
				return errors.New("forge.toml already exists")
			}
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return err
			}
			return os.WriteFile(path, []byte(stubManifest), 0o644)
		},
	})
}
