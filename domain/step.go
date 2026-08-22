package domain

// StepKind enumerates the supported build step types.
type StepKind string

const (
	// StepKindApt manages system packages via a package manager (apt-get).
	StepKindApt StepKind = "apt"
	// StepKindSource fetches source code and builds/installs it.
	StepKindSource StepKind = "source"
	// StepKindBinary downloads a prebuilt binary and copies it into place.
	StepKindBinary StepKind = "binary"
	// StepKindShell runs arbitrary shell commands.
	StepKindShell StepKind = "shell"
	// StepKindVerify asserts that files exist (verification step).
	StepKindVerify StepKind = "verify"
)

// Step is a single build operation in a manifest.
type Step struct {
	// Name is the unique name of the step, used for depends_on references.
	Name string
	// DependsOn lists the names of steps that must complete before this step.
	DependsOn []string
	// Kind is the type of step (apt, source, binary, shell, verify).
	Kind StepKind
	// Use references a named include group to splice as this step.
	Use string

	// Apt is populated when Kind == StepKindApt.
	Apt *AptStep
	// Source is populated when Kind == StepKindSource.
	Source *SourceStep
	// Binary is populated when Kind == StepKindBinary.
	Binary *BinaryStep
	// Shell is populated when Kind == StepKindShell.
	Shell *ShellStep
	// Verify is populated when Kind == StepKindVerify.
	Verify *VerifyStep
}

// AptStep manages system packages.
type AptStep struct {
	// Action is one of "install", "remove", or "purge".
	Action string
	// Packages is the list of package names.
	Packages []string
	// PackagesConditional is a list of version-gated package groups.
	PackagesConditional []ConditionalPackages
}

// ConditionalPackages gates a package list behind a shell condition.
// The condition is evaluated with ${VAR} interpolation before matching.
type ConditionalPackages struct {
	// Condition is a shell expression (e.g. "${POSTGRESQL_VERSION%%.*} >= 17").
	Condition string
	// Packages are installed only when the condition is truthy.
	Packages []string
}

// SourceStep fetches source and builds/installs it.
type SourceStep struct {
	// Fetch describes where the source comes from (git or archive).
	Fetch *FetchSpec
	// Build describes the build strategy and flags.
	Build *BuildSpec
	// Install controls whether to run the install step after building.
	Install bool
	// From references another source step's fetched source dir to reuse.
	From string
	// Dir is a subdirectory of the source root to build from (e.g. "contrib").
	Dir string
	// Env are extra environment variables for fetch/build/install.
	Env map[string]string
	// Verify lists post-install file assertions.
	Verify []VerifyCheck
}

// BinaryStep downloads a prebuilt binary and copies files into place.
type BinaryStep struct {
	// Fetch describes where the binary archive comes from.
	Fetch *FetchSpec
	// Install lists the copy operations.
	Install *BinaryInstall
	// Verify lists post-install file assertions.
	Verify []VerifyCheck
}

// BinaryInstall describes how to place a downloaded binary archive.
type BinaryInstall struct {
	// Copy is a list of file copy operations.
	Copy []CopySpec
}

// CopySpec copies a file from an extracted archive to a destination.
type CopySpec struct {
	// From is the relative path inside the extracted archive.
	From string
	// To is the absolute destination path.
	To string
	// Mode is the file mode as an octal string (e.g. "0755").
	Mode string
}

// ShellStep runs arbitrary shell commands.
type ShellStep struct {
	// Commands is the ordered list of shell commands to run.
	Commands []string
	// Env are extra environment variables for the commands.
	Env map[string]string
	// Dir is the working directory for the commands.
	Dir string
	// Verify lists post-command file assertions.
	Verify []VerifyCheck
}

// VerifyStep asserts that files exist.
type VerifyStep struct {
	// Checks lists the file-existence assertions.
	Checks []VerifyCheck
}

// VerifyCheck asserts a file exists after a step completes.
type VerifyCheck struct {
	// File is the absolute path expected to exist.
	File string
}
