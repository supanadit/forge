package domain

import "errors"

var (
	// ErrManifestNotFound is returned when the manifest file does not exist.
	ErrManifestNotFound = errors.New("manifest not found")
	// ErrInvalidManifest is returned when the manifest fails schema validation.
	ErrInvalidManifest = errors.New("invalid manifest")
	// ErrCircularDependency is returned when depends_on forms a cycle.
	ErrCircularDependency = errors.New("circular dependency detected")
	// ErrUnknownStepKind is returned when a step has an unsupported kind.
	ErrUnknownStepKind = errors.New("unknown step kind")
	// ErrStepFailed is returned when a build step fails.
	ErrStepFailed = errors.New("step failed")
	// ErrUnknownIncludeGroup is returned when a step references an unregistered group.
	ErrUnknownIncludeGroup = errors.New("unknown include group")
	// ErrUnknownDependency is returned when depends_on references a missing step.
	ErrUnknownDependency = errors.New("unknown step dependency")
	// ErrDuplicateStep is returned when two steps share a name.
	ErrDuplicateStep = errors.New("duplicate step name")
)
