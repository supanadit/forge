package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/supanadit/forge/domain"
)

// ValidateService is the delivery's contract with the validate use case.
//
//go:generate mockery --name ValidateService
type ValidateService interface {
	Validate(ctx context.Context, manifestPath string) (domain.ValidationResult, error)
}

// ValidateHandler is the terminal handler for `forge validate`.
type ValidateHandler struct {
	svc ValidateService
}

// NewValidateHandler constructs the handler and registers the validate subcommand.
func NewValidateHandler(root *cobra.Command, svc ValidateService) {
	h := &ValidateHandler{svc: svc}
	root.AddCommand(h.validateCmd())
}

func (h *ValidateHandler) validateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate <manifest.toml>",
		Short: "Validate a forge manifest without executing",
		Args:  cobra.ExactArgs(1),
		RunE:  h.run,
	}
}

func (h *ValidateHandler) run(cmd *cobra.Command, args []string) error {
	res, err := h.svc.Validate(cmd.Context(), args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ %s\n", err)
		return err
	}

	if len(res.Errors) > 0 {
		for _, e := range res.Errors {
			fmt.Fprintf(os.Stderr, "  ✗ %s\n", e)
		}
		return fmt.Errorf("%w: manifest has %d validation error(s)", domain.ErrInvalidManifest, len(res.Errors))
	}

	fmt.Printf("✅ Manifest valid: %d steps, %d includes\n", res.StepCount, res.IncludeCount)
	for _, w := range res.Warnings {
		fmt.Printf("  ⚠ %s\n", w)
	}
	return nil
}
