package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/supanadit/forge/domain"
)

// BuildService is the delivery's contract with the build use case.
// The delivery owns this interface — it is NOT the build.Service struct.
//
//go:generate mockery --name BuildService
type BuildService interface {
	Build(ctx context.Context, manifestPath string, opts domain.BuildOptions) (domain.BuildResult, error)
}

// BuildHandler is the terminal handler for `forge build`.
type BuildHandler struct {
	svc BuildService
}

// NewBuildHandler constructs the handler and registers the build subcommand.
func NewBuildHandler(root *cobra.Command, svc BuildService) {
	h := &BuildHandler{svc: svc}
	root.AddCommand(h.buildCmd())
}

func (h *BuildHandler) buildCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "build <manifest.toml>",
		Short: "Build a project from a forge manifest",
		Args:  cobra.ExactArgs(1),
		RunE:  h.run,
	}
	cmd.Flags().Int("parallel", 0, "Max parallel steps (0 = auto)")
	cmd.Flags().Bool("dry-run", false, "Print execution plan without running")
	cmd.Flags().Bool("fail-fast", true, "Stop on first step failure")
	cmd.Flags().BoolP("verbose", "v", false, "Stream live step output")
	cmd.Flags().StringArray("var", nil, "Override manifest var (KEY=VALUE)")
	return cmd
}

func (h *BuildHandler) run(cmd *cobra.Command, args []string) error {
	manifestPath := args[0]

	parallel, _ := cmd.Flags().GetInt("parallel")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	failFast, _ := cmd.Flags().GetBool("fail-fast")
	verbose, _ := cmd.Flags().GetBool("verbose")
	varOverrides, _ := cmd.Flags().GetStringArray("var")

	opts := domain.BuildOptions{
		Parallel:     parallel,
		DryRun:       dryRun,
		FailFast:     failFast,
		Verbose:      verbose,
		VarOverrides: varOverrides,
	}

	fmt.Printf("🔨 Building %s\n", manifestPath)
	if dryRun {
		// Dry run needs only the plan; run with a stub that skips execution.
		res, err := h.svc.Build(cmd.Context(), manifestPath, opts)
		if err != nil {
			return err
		}
		printBuildResult(res)
		return nil
	}

	res, err := h.svc.Build(cmd.Context(), manifestPath, opts)
	if err != nil {
		printBuildResult(res)
		fmt.Fprintf(os.Stderr, "❌ %s\n", err)
		return err
	}
	printBuildResult(res)
	return nil
}

func printBuildResult(res domain.BuildResult) {
	for _, sr := range res.Steps {
		switch sr.Status {
		case domain.StepStatusSuccess:
			fmt.Printf("  ✓ %s (%v)\n", sr.Name, sr.Duration.Round(time.Millisecond))
		case domain.StepStatusFailed:
			fmt.Printf("  ✗ %s (%v)\n", sr.Name, sr.Duration.Round(time.Millisecond))
		case domain.StepStatusSkipped:
			fmt.Printf("  - %s skipped\n", sr.Name)
		default:
			fmt.Printf("  ? %s (%v)\n", sr.Name, sr.Duration.Round(time.Millisecond))
		}
	}
	fmt.Printf("✅ Build complete: %d steps in %v\n", len(res.Steps), res.TotalDuration.Round(time.Millisecond))
}
