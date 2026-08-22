// Package main is forge's composition root. It is the only place that knows
// which concrete drivers implement the module interfaces, and wires them via
// fx into the use cases and the CLI delivery layer.
package main

import (
	"context"
	"fmt"
	"os"

	"go.uber.org/fx"

	"github.com/supanadit/forge/build"
	"github.com/supanadit/forge/builder"
	"github.com/supanadit/forge/fetch"
	"github.com/supanadit/forge/internal/cli"
	"github.com/supanadit/forge/internal/repository/disk"
	"github.com/supanadit/forge/internal/shutdown"
	"github.com/supanadit/forge/manifest"
	"github.com/supanadit/forge/scheduler"
	"github.com/supanadit/forge/validate"
)

func main() {
	mgr := shutdown.New(shutdown.DefaultSignals...)
	shutdownCtx := mgr.Context()

	go func() {
		sig := mgr.Wait()
		shutdown.PrintInterrupted(sig)
	}()

	options := []fx.Option{
		fx.NopLogger,
		fx.Supply(shutdownCtx),
		fx.Provide(
			// Drivers (infrastructure — the only place concrete impls are known).
			fx.Annotate(disk.NewManifestRepository, fx.As(new(manifest.ManifestRepository))),
			fx.Annotate(disk.NewFetchRepository, fx.As(new(fetch.FetchRepository))),
			fx.Annotate(disk.NewBuildRepository, fx.As(new(builder.BuildRepository))),
			fx.Annotate(disk.NewExecutor, fx.As(new(build.StepExecutor))),

			// Use cases (driver-agnostic).
			manifest.NewService,
			fetch.NewService,
			builder.NewService,
			scheduler.NewService,
			fx.Annotate(build.NewService, fx.As(new(cli.BuildService))),
			fx.Annotate(validate.NewService, fx.As(new(cli.ValidateService))),

			// Delivery.
			cli.NewRootCmd,
		),
		fx.Invoke(
			cli.RegisterRootCmd,
			cli.NewBuildHandler,
			cli.NewValidateHandler,
			cli.NewInitHandler,
		),
	}

	app := fx.New(options...)

	if err := app.Start(context.Background()); err != nil {
		mgr.Stop()
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	mgr.Stop()

	if err := app.Stop(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
