package cli_test

import (
	"context"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/supanadit/forge/domain"
	"github.com/supanadit/forge/internal/cli"
	"github.com/supanadit/forge/internal/cli/mocks"
)

func newRoot() *cobra.Command {
	root := cli.NewRootCmd()
	root.SetArgs([]string{})
	return root
}

func TestBuildHandler_Success(t *testing.T) {
	mockSvc := new(mocks.BuildService)
	mockSvc.On("Build", mock.Anything, "forge.toml", mock.AnythingOfType("domain.BuildOptions")).
		Return(domain.BuildResult{
			Steps: []domain.StepResult{{Name: "install-deps", Status: domain.StepStatusSuccess}},
		}, nil)

	root := newRoot()
	cli.NewBuildHandler(root, mockSvc)

	root.SetArgs([]string{"build", "forge.toml"})
	err := root.Execute()
	require.NoError(t, err)
	mockSvc.AssertExpectations(t)
}

func TestBuildHandler_StepFailure(t *testing.T) {
	mockSvc := new(mocks.BuildService)
	mockSvc.On("Build", mock.Anything, "bad.toml", mock.AnythingOfType("domain.BuildOptions")).
		Return(domain.BuildResult{}, domain.ErrStepFailed)

	root := newRoot()
	cli.NewBuildHandler(root, mockSvc)

	root.SetArgs([]string{"build", "bad.toml"})
	err := root.Execute()
	assert.ErrorIs(t, err, domain.ErrStepFailed)
	mockSvc.AssertExpectations(t)
}

func TestBuildHandler_DryRunFlagPassed(t *testing.T) {
	mockSvc := new(mocks.BuildService)
	mockSvc.On("Build", mock.Anything, "forge.toml", mock.MatchedBy(func(o domain.BuildOptions) bool {
		return o.DryRun && o.FailFast
	})).Return(domain.BuildResult{Steps: []domain.StepResult{}}, nil)

	root := newRoot()
	cli.NewBuildHandler(root, mockSvc)

	root.SetArgs([]string{"build", "--dry-run", "forge.toml"})
	err := root.Execute()
	require.NoError(t, err)
	mockSvc.AssertExpectations(t)
}

func TestBuildHandler_RequiresManifestArg(t *testing.T) {
	mockSvc := new(mocks.BuildService)
	root := newRoot()
	cli.NewBuildHandler(root, mockSvc)

	root.SetArgs([]string{"build"})
	err := root.Execute()
	assert.Error(t, err)
	mockSvc.AssertNotCalled(t, "Build", mock.Anything, mock.Anything, mock.Anything)
}

func TestValidateHandler_Valid(t *testing.T) {
	mockSvc := new(mocks.ValidateService)
	mockSvc.On("Validate", mock.Anything, "forge.toml").
		Return(domain.ValidationResult{StepCount: 3, IncludeCount: 1}, nil)

	root := newRoot()
	cli.NewValidateHandler(root, mockSvc)

	root.SetArgs([]string{"validate", "forge.toml"})
	err := root.Execute()
	require.NoError(t, err)
	mockSvc.AssertExpectations(t)
}

func TestValidateHandler_Invalid(t *testing.T) {
	mockSvc := new(mocks.ValidateService)
	mockSvc.On("Validate", mock.Anything, "bad.toml").
		Return(domain.ValidationResult{StepCount: 1, Errors: []string{"step has unsupported kind"}}, nil)

	root := newRoot()
	cli.NewValidateHandler(root, mockSvc)

	root.SetArgs([]string{"validate", "bad.toml"})
	err := root.Execute()
	assert.Error(t, err)
	mockSvc.AssertExpectations(t)
}

func TestExitCodeMapping(t *testing.T) {
	cases := []struct {
		err  error
		want int
	}{
		{domain.ErrManifestNotFound, 2},
		{domain.ErrInvalidManifest, 3},
		{domain.ErrCircularDependency, 4},
		{domain.ErrUnknownDependency, 5},
		{domain.ErrDuplicateStep, 6},
		{domain.ErrStepFailed, 7},
		{context.DeadlineExceeded, 1},
	}
	for _, c := range cases {
		got := cli.ExitCode(c.err)
		assert.Equal(t, c.want, got, "exitCode(%v)", c.err)
	}
}
