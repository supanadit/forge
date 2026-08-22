package disk

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/supanadit/forge/domain"
)

// BuildRepository compiles source trees using autotools, cmake, meson, or make.
type BuildRepository struct{}

// NewBuildRepository creates a filesystem-backed build repository.
func NewBuildRepository() *BuildRepository {
	return &BuildRepository{}
}

// Build compiles the source at sourceDir, returning the build directory.
func (r *BuildRepository) Build(ctx context.Context, spec domain.BuildSpec, sourceDir string, env []string) (string, error) {
	strategy := spec.Strategy
	if strategy == "" || strategy == domain.BuildStrategyDetect {
		strategy = detectStrategy(sourceDir)
	}

	switch strategy {
	case domain.BuildStrategyConfigure:
		return r.buildConfigure(ctx, spec, sourceDir, env)
	case domain.BuildStrategyAutogen:
		return r.buildAutogen(ctx, spec, sourceDir, env)
	case domain.BuildStrategyCMake:
		return r.buildCMake(ctx, spec, sourceDir, env)
	case domain.BuildStrategyMeson:
		return r.buildMeson(ctx, spec, sourceDir, env)
	case domain.BuildStrategyMake:
		return r.buildMake(ctx, spec, sourceDir, env)
	default:
		return "", fmt.Errorf("unsupported build strategy %q", strategy)
	}
}

// Install runs `make install` in the build directory.
func (r *BuildRepository) Install(ctx context.Context, buildDir string, prefix string) error {
	install := exec.CommandContext(ctx, "make", "install")
	install.Dir = buildDir
	if out, err := install.CombinedOutput(); err != nil {
		return fmt.Errorf("make install: %w\n%s", err, out)
	}
	return nil
}

func (r *BuildRepository) buildConfigure(ctx context.Context, spec domain.BuildSpec, src string, env []string) (string, error) {
	configure := filepath.Join(src, "configure")
	if _, err := os.Stat(configure); err != nil {
		return "", fmt.Errorf("configure not found in %s", src)
	}
	if err := os.Chmod(configure, 0o755); err != nil {
		return "", err
	}

	args := []string{configure}
	if spec.Prefix != "" {
		args = append(args, "--prefix="+spec.Prefix)
	}
	args = append(args, spec.Flags...)

	if err := runCmd(ctx, exec.CommandContext(ctx, "bash", args...), src, env); err != nil {
		return "", fmt.Errorf("configure: %w", err)
	}
	if err := r.runMake(ctx, spec, src, env); err != nil {
		return "", err
	}
	return src, nil
}

func (r *BuildRepository) buildAutogen(ctx context.Context, spec domain.BuildSpec, src string, env []string) (string, error) {
	if err := runCmd(ctx, exec.CommandContext(ctx, "autoreconf", "-fi"), src, env); err != nil {
		return "", fmt.Errorf("autoreconf: %w", err)
	}
	return r.buildConfigure(ctx, spec, src, env)
}

func (r *BuildRepository) buildCMake(ctx context.Context, spec domain.BuildSpec, src string, env []string) (string, error) {
	buildDir := filepath.Join(src, "build")
	if err := os.MkdirAll(buildDir, 0o755); err != nil {
		return "", err
	}
	args := []string{src}
	if spec.Prefix != "" {
		args = append(args, "-DCMAKE_INSTALL_PREFIX="+spec.Prefix)
	}
	args = append(args, "-DCMAKE_BUILD_TYPE=Release")
	args = append(args, spec.Flags...)

	cmake := exec.CommandContext(ctx, "cmake", args...)
	cmake.Dir = buildDir
	cmake.Env = mergeEnv(env)
	if err := runCmd(ctx, cmake, buildDir, env); err != nil {
		return "", fmt.Errorf("cmake: %w", err)
	}
	if err := r.runMake(ctx, spec, buildDir, env); err != nil {
		return "", err
	}
	return buildDir, nil
}

func (r *BuildRepository) buildMeson(ctx context.Context, spec domain.BuildSpec, src string, env []string) (string, error) {
	buildDir := filepath.Join(src, "build")
	if err := os.MkdirAll(buildDir, 0o755); err != nil {
		return "", err
	}
	args := []string{"setup", buildDir, src}
	if spec.Prefix != "" {
		args = append(args, "--prefix="+spec.Prefix)
	}
	args = append(args, spec.Flags...)
	if err := runCmd(ctx, exec.CommandContext(ctx, "meson", args...), src, env); err != nil {
		return "", fmt.Errorf("meson setup: %w", err)
	}
	if err := runCmd(ctx, exec.CommandContext(ctx, "ninja", "-C", buildDir), buildDir, env); err != nil {
		return "", fmt.Errorf("ninja: %w", err)
	}
	return buildDir, nil
}

func (r *BuildRepository) buildMake(ctx context.Context, spec domain.BuildSpec, src string, env []string) (string, error) {
	if err := r.runMake(ctx, spec, src, env); err != nil {
		return "", err
	}
	return src, nil
}

func (r *BuildRepository) runMake(ctx context.Context, spec domain.BuildSpec, dir string, env []string) error {
	args := []string{}
	if spec.Jobs > 0 {
		args = append(args, fmt.Sprintf("-j%d", spec.Jobs))
	}
	args = append(args, spec.MakeFlags...)
	make := exec.CommandContext(ctx, "make", args...)
	make.Dir = dir
	make.Env = mergeEnv(env)
	if err := runCmd(ctx, make, dir, env); err != nil {
		return fmt.Errorf("make: %w", err)
	}
	return nil
}

func runCmd(ctx context.Context, cmd *exec.Cmd, dir string, env []string) error {
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = mergeEnv(env)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%v: %w\n%s", cmd.Args, err, out)
	}
	return nil
}

func mergeEnv(extra []string) []string {
	if len(extra) == 0 {
		return nil
	}
	return append(os.Environ(), extra...)
}

func detectStrategy(src string) domain.BuildStrategy {
	if _, err := os.Stat(filepath.Join(src, "CMakeLists.txt")); err == nil {
		return domain.BuildStrategyCMake
	}
	if _, err := os.Stat(filepath.Join(src, "meson.build")); err == nil {
		return domain.BuildStrategyMeson
	}
	if _, err := os.Stat(filepath.Join(src, "configure")); err == nil {
		return domain.BuildStrategyConfigure
	}
	if _, err := os.Stat(filepath.Join(src, "Makefile")); err == nil {
		return domain.BuildStrategyMake
	}
	return domain.BuildStrategyMake
}
