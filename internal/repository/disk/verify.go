package disk

import (
	"fmt"
	"os"

	"github.com/supanadit/forge/domain"
	"github.com/supanadit/forge/internal/repository"
)

// executeVerify asserts that each listed file exists after a step.
func (e *Executor) executeVerify(checks []domain.VerifyCheck, lookup repository.Lookup) error {
	for _, c := range checks {
		path := repository.Replace(c.File, lookup)
		if _, err := os.Stat(path); err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("verify: expected file not found: %s", path)
			}
			return fmt.Errorf("verify %s: %w", path, err)
		}
	}
	return nil
}
