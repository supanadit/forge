package scheduler

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/supanadit/forge/domain"
)

func TestSchedule_LinearChain(t *testing.T) {
	s := NewService()
	plan, err := s.Schedule([]domain.Step{
		{Name: "a"},
		{Name: "b", DependsOn: []string{"a"}},
		{Name: "c", DependsOn: []string{"b"}},
	})
	assert.NoError(t, err)
	assert.Equal(t, [][]string{{"a"}, {"b"}, {"c"}}, plan.Levels)
	assert.Equal(t, []string{"a", "b", "c"}, plan.Order)
}

func TestSchedule_IndependentStepsShareLevel(t *testing.T) {
	s := NewService()
	plan, err := s.Schedule([]domain.Step{
		{Name: "a"},
		{Name: "b"},
		{Name: "c"},
	})
	assert.NoError(t, err)
	assert.Equal(t, [][]string{{"a", "b", "c"}}, plan.Levels)
}

func TestSchedule_DeterministicOrdering(t *testing.T) {
	s := NewService()
	plan, err := s.Schedule([]domain.Step{
		{Name: "z"},
		{Name: "a"},
		{Name: "m"},
	})
	assert.NoError(t, err)
	// Deterministic: sorted within a level.
	assert.Equal(t, []string{"a", "m", "z"}, plan.Order)
}

func TestSchedule_DiamondDeps(t *testing.T) {
	s := NewService()
	plan, err := s.Schedule([]domain.Step{
		{Name: "root"},
		{Name: "l", DependsOn: []string{"root"}},
		{Name: "r", DependsOn: []string{"root"}},
		{Name: "leaf", DependsOn: []string{"l", "r"}},
	})
	assert.NoError(t, err)
	assert.Equal(t, []string{"root"}, plan.Levels[0])
	assert.ElementsMatch(t, []string{"l", "r"}, plan.Levels[1])
	assert.Equal(t, []string{"leaf"}, plan.Levels[2])
}

func TestSchedule_CircularDependency(t *testing.T) {
	s := NewService()
	_, err := s.Schedule([]domain.Step{
		{Name: "a", DependsOn: []string{"b"}},
		{Name: "b", DependsOn: []string{"a"}},
	})
	assert.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrCircularDependency))
}

func TestSchedule_UnknownDependency(t *testing.T) {
	s := NewService()
	_, err := s.Schedule([]domain.Step{
		{Name: "a", DependsOn: []string{"ghost"}},
	})
	assert.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrUnknownDependency))
}

func TestSchedule_DuplicateNames(t *testing.T) {
	s := NewService()
	_, err := s.Schedule([]domain.Step{
		{Name: "a"},
		{Name: "a"},
	})
	assert.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrDuplicateStep))
}

func TestSchedule_SelfDependencyIsCycle(t *testing.T) {
	s := NewService()
	_, err := s.Schedule([]domain.Step{
		{Name: "a", DependsOn: []string{"a"}},
	})
	assert.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrCircularDependency))
}

func TestSchedule_Empty(t *testing.T) {
	s := NewService()
	plan, err := s.Schedule(nil)
	assert.NoError(t, err)
	assert.Empty(t, plan.Levels)
	assert.Empty(t, plan.Order)
}
