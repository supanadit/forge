// Package scheduler is a pure use case that computes the execution plan for a
// manifest's steps.
//
// It has no repository and no I/O: it performs a topological sort over the
// steps' depends_on edges, detects cycles and unknown dependencies, and groups
// steps into parallel levels. The build use case consumes the plan.
package scheduler

import (
	"fmt"
	"sort"

	"github.com/supanadit/forge/domain"
)

// Service schedules steps into an execution plan.
type Service struct{}

// NewService creates a scheduler service.
func NewService() *Service {
	return &Service{}
}

// Schedule topologically sorts the steps and groups independent steps into
// parallel levels. It returns an error on cycles, duplicate names, or
// references to unknown dependencies.
func (s *Service) Schedule(steps []domain.Step) (domain.ExecutionPlan, error) {
	byName := make(map[string]domain.Step, len(steps))
	for _, st := range steps {
		if _, dup := byName[st.Name]; dup {
			return domain.ExecutionPlan{}, fmt.Errorf("%w: %q", domain.ErrDuplicateStep, st.Name)
		}
		byName[st.Name] = st
	}

	// Verify every depends_on target exists.
	for _, st := range steps {
		for _, dep := range st.DependsOn {
			if _, ok := byName[dep]; !ok {
				return domain.ExecutionPlan{}, fmt.Errorf("%w: step %q depends on unknown step %q", domain.ErrUnknownDependency, st.Name, dep)
			}
		}
	}

	// Kahn's algorithm with in-degree tracking for levels.
	indeg := make(map[string]int, len(steps))
	dependents := make(map[string][]string, len(steps))
	for _, st := range steps {
		indeg[st.Name] = len(st.DependsOn)
		for _, dep := range st.DependsOn {
			dependents[dep] = append(dependents[dep], st.Name)
		}
	}

	// Initialize the frontier with steps that have no dependencies.
	var frontier []string
	for _, st := range steps {
		if indeg[st.Name] == 0 {
			frontier = append(frontier, st.Name)
		}
	}
	sort.Strings(frontier) // deterministic ordering within a level

	plan := domain.ExecutionPlan{}
	processed := 0

	for len(frontier) > 0 {
		plan.Levels = append(plan.Levels, frontier)
		var next []string
		for _, name := range frontier {
			plan.Order = append(plan.Order, name)
			processed++
			for _, dep := range dependents[name] {
				indeg[dep]--
				if indeg[dep] == 0 {
					next = append(next, dep)
				}
			}
		}
		sort.Strings(next)
		frontier = next
	}

	if processed != len(steps) {
		return domain.ExecutionPlan{}, fmt.Errorf("%w: %d steps not schedulable", domain.ErrCircularDependency, len(steps)-processed)
	}

	return plan, nil
}
