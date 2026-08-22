package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// CacheCleaner is the delivery's contract for cache maintenance operations.
// The delivery owns this interface; it is NOT the cache.Service struct.
//
//go:generate mockery --name CacheCleaner
type CacheCleaner interface {
	// Prune removes cache entries for a project. When validSteps is non-nil,
	// only entries whose step names are not in the set are removed.
	Prune(project string, validSteps map[string]bool) error
	// List returns the cache entry paths for a project.
	List(project string) ([]string, error)
}

// CacheHandler is the terminal handler for `forge cache`.
type CacheHandler struct {
	svc CacheCleaner
}

// NewCacheHandler constructs the handler and registers the cache subcommand.
func NewCacheHandler(root *cobra.Command, svc CacheCleaner) {
	h := &CacheHandler{svc: svc}
	root.AddCommand(h.cacheCmd())
}

func (h *CacheHandler) cacheCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cache",
		Short: "Manage the build cache",
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "clean [--project <name>]",
			Short: "Remove cache entries for a project",
			RunE:  h.runClean,
		},
		&cobra.Command{
			Use:   "list [--project <name>]",
			Short: "List cached steps for a project",
			RunE:  h.runList,
		},
	)
	cmd.PersistentFlags().String("project", "", "Project name to scope the operation")
	return cmd
}

func (h *CacheHandler) runClean(cmd *cobra.Command, _ []string) error {
	project, _ := cmd.Flags().GetString("project")
	if project == "" {
		return fmt.Errorf("--project is required")
	}
	if err := h.svc.Prune(project, nil); err != nil {
		return err
	}
	fmt.Printf("🧹 Removed cache entries for project %q\n", project)
	return nil
}

func (h *CacheHandler) runList(cmd *cobra.Command, _ []string) error {
	project, _ := cmd.Flags().GetString("project")
	if project == "" {
		return fmt.Errorf("--project is required")
	}
	entries, err := h.svc.List(project)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		fmt.Printf("no cached steps for project %q\n", project)
		return nil
	}
	for _, e := range entries {
		fmt.Println(e)
	}
	return nil
}
