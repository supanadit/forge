package domain

import "time"

// StepStatus enumerates the execution status of a build step.
type StepStatus string

const (
	// StepStatusPending means the step has not started.
	StepStatusPending StepStatus = "pending"
	// StepStatusRunning means the step is currently executing.
	StepStatusRunning StepStatus = "running"
	// StepStatusSuccess means the step completed successfully.
	StepStatusSuccess StepStatus = "success"
	// StepStatusFailed means the step failed.
	StepStatusFailed StepStatus = "failed"
	// StepStatusSkipped means the step was skipped (e.g. already satisfied).
	StepStatusSkipped StepStatus = "skipped"
	// StepStatusCached means the step was restored from the build cache.
	StepStatusCached StepStatus = "cached"
)

// StepResult captures the outcome of executing one step.
type StepResult struct {
	// Name is the step name.
	Name string
	// Status is the execution status.
	Status StepStatus
	// Duration is how long the step took.
	Duration time.Duration
	// Err is set when Status == StepStatusFailed.
	Err error
	// SourceDir is the resolved source directory for source steps,
	// reusable by later steps via the `from` field.
	SourceDir string
	// Prefix is the install prefix for source steps,
	// referenceable via ${step:NAME} interpolation.
	Prefix string
	// Outputs lists filesystem paths the step created or modified, discovered
	// automatically by diffing filesystem snapshots around the step (shell
	// steps). The build cache persists them so a later run can verify a
	// cached result without manual cache_verify declarations.
	Outputs []string
	// CacheKey is the content hash that uniquely identifies this step's inputs.
	// Dependent steps use it in their own cache keys for transitive invalidation.
	CacheKey string
}

// BuildResult is the aggregate outcome of building a manifest.
type BuildResult struct {
	// Steps are the per-step results in topological execution order.
	Steps []StepResult
	// TotalDuration is the wall-clock time of the whole build.
	TotalDuration time.Duration
}

// BuildOptions configure a build run.
type BuildOptions struct {
	// Parallel is the max number of concurrently executed steps (0 = auto).
	Parallel int
	// DryRun prints the execution plan without running anything.
	DryRun bool
	// FailFast stops on the first failed step (default true).
	FailFast bool
	// Verbose streams live command output to the terminal.
	Verbose bool
	// VarOverrides are KEY=VALUE overrides applied on top of [vars] and env.
	VarOverrides []string
	// CacheDir overrides the build cache directory. Empty uses the default.
	CacheDir string
	// NoCache disables the build cache; all steps run regardless.
	NoCache bool
	// PkgManager overrides OS package-manager detection (apt, dnf, yum, apk).
	PkgManager string
}

// ValidationResult is the outcome of validating a manifest without running it.
type ValidationResult struct {
	// StepCount is the number of steps in the resolved manifest.
	StepCount int
	// IncludeCount is the number of include sources that contributed steps.
	IncludeCount int
	// Errors are fatal validation problems.
	Errors []string
	// Warnings are non-fatal concerns.
	Warnings []string
}

// ExecutionPlan is the topologically sorted execution order of steps.
type ExecutionPlan struct {
	// Levels are groups of step names that may run in parallel.
	// Steps within a level have no dependency on each other.
	Levels [][]string
	// Order is the flattened topological order of step names.
	Order []string
}

// StepContext carries runtime state into a step executor.
type StepContext struct {
	// Vars are the resolved build variables (vars + env + overrides).
	Vars map[string]string
	// Previous maps step names to their completed StepResults.
	Previous map[string]StepResult
	// WorkDir is the base working directory (defaults to the manifest dir).
	WorkDir string
	// Verbose streams live command output to the terminal when true.
	Verbose bool
	// Project is the manifest's project name, used to scope the cache.
	Project string
	// CacheDir overrides the build cache directory. Empty uses the default.
	CacheDir string
	// NoCache disables the build cache for this step.
	NoCache bool
	// PkgManager overrides OS package-manager detection (apt, dnf, yum, apk).
	PkgManager string
}
