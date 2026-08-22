// Package cli is the terminal delivery layer for forge.
//
// It mirrors internal/rest in the go-clean-arch reference, adapted for a
// terminal (cobra) instead of HTTP (echo). It owns the service interfaces it
// needs from the use cases (BuildService, ValidateService) and holds them as
// interfaces — never as concrete structs. It contains no business logic; it
// only translates CLI input into use case calls and CLI output.
package cli

import (
	"context"

	"github.com/spf13/cobra"

	"go.uber.org/fx"
)

// NewRootCmd returns the root forge command.
func NewRootCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "forge",
		Short: "Declarative TOML-driven build orchestrator",
	}
}

// RegisterRootCmd executes the root command via the fx lifecycle.
func RegisterRootCmd(rootCmd *cobra.Command, lc fx.Lifecycle) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			return rootCmd.Execute()
		},
	})
}
