package build

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/supanadit/forge/build/mocks"
	"github.com/supanadit/forge/domain"
	"github.com/supanadit/forge/manifest"
	"github.com/supanadit/forge/scheduler"
)

func newTestService(t *testing.T, m *manifest.Service, exec StepExecutor) *Service {
	t.Helper()
	return NewService(m, scheduler.NewService(), exec)
}

func TestBuild_SuccessExecutesInOrder(t *testing.T) {
	ctx := context.Background()
	mockExec := new(mocks.StepExecutor)
	mockExec.On("Execute", mock.Anything, mock.AnythingOfType("domain.Step"), mock.AnythingOfType("domain.StepContext")).
		Return(domain.StepResult{Status: domain.StepStatusSuccess}, nil).
		Times(3)

	m := manifest.NewService(&memoryManifestRepo{})
	svc := newTestService(t, m, mockExec)

	res, err := svc.Build(ctx, "simple", domain.BuildOptions{})
	require.NoError(t, err)
	assert.Len(t, res.Steps, 3)
	for _, sr := range res.Steps {
		assert.Equal(t, domain.StepStatusSuccess, sr.Status)
	}
	mockExec.AssertExpectations(t)
}

func TestBuild_DryRunSkipsExecution(t *testing.T) {
	ctx := context.Background()
	mockExec := new(mocks.StepExecutor)

	m := manifest.NewService(&memoryManifestRepo{})
	svc := newTestService(t, m, mockExec)

	res, err := svc.Build(ctx, "simple", domain.BuildOptions{DryRun: true})
	require.NoError(t, err)
	assert.Len(t, res.Steps, 3)
	for _, sr := range res.Steps {
		assert.Equal(t, domain.StepStatusPending, sr.Status)
	}
	mockExec.AssertNotCalled(t, "Execute", mock.Anything, mock.Anything, mock.Anything)
}

func TestBuild_FailFast(t *testing.T) {
	ctx := context.Background()
	mockExec := new(mocks.StepExecutor)
	mockExec.On("Execute", mock.Anything, mock.AnythingOfType("domain.Step"), mock.AnythingOfType("domain.StepContext")).
		Return(domain.StepResult{}, domain.ErrStepFailed).
		Once()

	m := manifest.NewService(&chainManifestRepo{})
	svc := newTestService(t, m, mockExec)

	_, err := svc.Build(ctx, "chain", domain.BuildOptions{FailFast: true})
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrStepFailed)
	mockExec.AssertExpectations(t)
}

// chainManifestRepo returns steps in a linear dependency chain so fail-fast
// stops all downstream levels.
type chainManifestRepo struct{}

func (r *chainManifestRepo) Load(ctx context.Context, path string) (domain.Manifest, error) {
	return domain.Manifest{
		Project: domain.Project{Name: "chain"},
		Steps: []domain.Step{
			{Name: "one", Kind: domain.StepKindShell, Shell: &domain.ShellStep{Commands: []string{"echo 1"}}},
			{Name: "two", DependsOn: []string{"one"}, Kind: domain.StepKindShell, Shell: &domain.ShellStep{Commands: []string{"echo 2"}}},
			{Name: "three", DependsOn: []string{"two"}, Kind: domain.StepKindShell, Shell: &domain.ShellStep{Commands: []string{"echo 3"}}},
		},
	}, nil
}

// memoryManifestRepo is a minimal fake manifest repository for build tests.
type memoryManifestRepo struct{}

func (r *memoryManifestRepo) Load(ctx context.Context, path string) (domain.Manifest, error) {
	return domain.Manifest{
		Project: domain.Project{Name: "test"},
		Steps: []domain.Step{
			{Name: "one", Kind: domain.StepKindShell, Shell: &domain.ShellStep{Commands: []string{"echo 1"}}},
			{Name: "two", Kind: domain.StepKindShell, Shell: &domain.ShellStep{Commands: []string{"echo 2"}}},
			{Name: "three", DependsOn: []string{"one", "two"}, Kind: domain.StepKindShell, Shell: &domain.ShellStep{Commands: []string{"echo 3"}}},
		},
	}, nil
}
