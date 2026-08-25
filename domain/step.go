package domain

// Step is a single component: an ordered list of operations.
type Step struct {
	// Name is the unique name of the step, used for depends_on references.
	Name string
	// DependsOn lists the names of steps that must complete before this step.
	DependsOn []string
	// Ops is the ordered list of operations that make up this component.
	Ops []Operation
}

// InstallOp fetches and builds/installs when set: apt, source, or binary.
type InstallOp struct {
	// Apt installs apt packages when set.
	Apt *AptInstall
	// Source fetches and builds source when set.
	Source *SourceInstall
	// Binary downloads and copies a binary when set.
	Binary *BinaryInstall
}

// AptInstall installs apt packages, classified into build and runtime.
type AptInstall struct {
	// Build packages are installed, then removed after all components finish.
	Build []string
	// Runtime packages are installed and kept (marked manually-installed).
	Runtime []string
	// Conditional gates packages behind a structured version condition.
	Conditional []ConditionalApt
}

// ConditionalApt gates a package list behind a structured version condition.
type ConditionalApt struct {
	// Category is "build" or "runtime" — which list the packages belong to.
	Category string
	// When is the structured condition.
	When VersionCondition
	// Packages are installed only when the condition is true.
	Packages []string
}

// VersionCondition is a structured, deterministic version comparison.
type VersionCondition struct {
	// Var is the variable to compare (e.g. "POSTGRESQL_VERSION").
	Var string
	// Gte/Lte/Gt/Lt/Eq are the comparison operators; exactly one is set.
	Gte string
	Lte string
	Gt  string
	Lt  string
	Eq  string
}

// BuildStrategyNone skips the build and install phases; the component's
// before/after ops do the work (e.g. Ruby gems, pip installs).
const BuildStrategyNone BuildStrategy = "none"

// SourceInstall fetches source and builds/installs it.
type SourceInstall struct {
	// Type is the fetch type: "archive" or "git".
	Type string
	// Source is the URL (archive) or git URL.
	Source string
	// Ref is the git ref/branch/tag (git only).
	Ref string
	// Strategy is the build strategy (make/configure/meson/cmake/autogen).
	Strategy string
	// Flags are build flags.
	Flags []string
	// Prefix is the install prefix.
	Prefix string
	// Jobs is the parallel job count.
	Jobs int
	// InstallTarget is a custom make install target.
	InstallTarget string
	// Env are extra environment variables.
	Env map[string]string
	// Verify lists post-install file assertions.
	Verify []VerifyCheck
	// Before are ops run after fetch, before build (in the source dir).
	Before []Operation
	// After are ops run after install (in the source dir).
	After []Operation
}

// BinaryInstall downloads a prebuilt binary and copies files into place.
type BinaryInstall struct {
	// Source is the archive URL.
	Source string
	// Copy lists the copy operations.
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

// Operation is one atomic action: a raw shell command OR a declarative op.
type Operation struct {
	// Raw is a shell command run via `sh -c` when set.
	Raw string
	// User creates a system user when set.
	User *UserOp
	// Mkdir creates directories when set.
	Mkdir []MkdirOp
	// Chown changes ownership when set.
	Chown []ChownOp
	// Chmod changes permissions when set.
	Chmod []ChmodOp
	// Copy copies files when set.
	Copy []CopyOp
	// Touch creates empty files when set.
	Touch []string
	// Apt installs/removes apt packages when set.
	Apt *AptOp
	// Install fetches and builds/installs when set.
	Install *InstallOp
	// Verify asserts files exist when set.
	Verify []VerifyCheck
	// Generate runs a code generator when set.
	Generate *GenerateOp
}

// VerifyCheck asserts a file exists after a step completes.
type VerifyCheck struct {
	// File is the absolute path expected to exist.
	File string
}

// AptOp installs or removes apt packages.
type AptOp struct {
	// Action is "install" or "remove".
	Action string
	// Packages is the list of package names.
	Packages []string
}

// UserOp creates a system user via useradd.
type UserOp struct {
	Name       string
	CreateHome bool
	System     bool
	Shell      string
}

// MkdirOp creates a directory.
type MkdirOp struct {
	Path  string
	Mode  string
	Owner string
}

// ChownOp changes ownership.
type ChownOp struct {
	Path      string
	Owner     string
	Group     string
	Recursive bool
}

// ChmodOp changes permissions.
type ChmodOp struct {
	Path string
	Mode string
}

// CopyOp copies a file.
type CopyOp struct {
	From string
	To   string
	Mode string
}

// GenerateOp runs a code generator (e.g. protoc-c).
type GenerateOp struct {
	// Tool is the generator binary to run.
	Tool string
	// Input is the input file (relative to the source dir).
	Input string
	// Out is the output directory (relative to the source dir).
	Out string
	// Flags are extra arguments to the tool.
	Flags []string
}
