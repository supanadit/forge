package cli

import (
	"errors"

	"github.com/supanadit/forge/domain"
)

// ExitCode maps domain errors to process exit codes.
// Mirrors rest.getStatusCode in go-clean-arch, translating domain errors to
// terminal-appropriate exit codes instead of HTTP status codes.
func ExitCode(err error) int {
	switch {
	case errors.Is(err, domain.ErrManifestNotFound):
		return 2
	case errors.Is(err, domain.ErrInvalidManifest):
		return 3
	case errors.Is(err, domain.ErrCircularDependency):
		return 4
	case errors.Is(err, domain.ErrUnknownDependency):
		return 5
	case errors.Is(err, domain.ErrDuplicateStep):
		return 6
	case errors.Is(err, domain.ErrStepFailed):
		return 7
	default:
		return 1
	}
}
