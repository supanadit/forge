package disk

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/supanadit/forge/domain"
	"github.com/supanadit/forge/internal/repository"
)

// executeUser creates a system user via useradd. It is idempotent: if the
// user already exists, it is not an error.
func (e *Executor) executeUser(ctx context.Context, u *domain.UserOp, verbose bool) error {
	if u == nil || u.Name == "" {
		return fmt.Errorf("user step has no user")
	}
	args := []string{}
	if u.System {
		args = append(args, "-r")
	}
	if u.CreateHome {
		args = append(args, "-m")
	}
	if u.Shell != "" {
		args = append(args, "-s", u.Shell)
	}
	args = append(args, u.Name)
	cmd := exec.Command("useradd", args...)
	out, err := runProcess(ctx, cmd)
	if err != nil {
		// useradd exits non-zero if the user already exists; treat as success.
		if strings.Contains(string(out), "already exists") {
			return nil
		}
		return fmt.Errorf("useradd %s: %w\n%s", u.Name, err, out)
	}
	return nil
}

// executeFS performs filesystem operations in order.
func (e *Executor) executeFS(ctx context.Context, mkdir []domain.MkdirOp, chown []domain.ChownOp, chmod []domain.ChmodOp, copy []domain.CopyOp, touch []string, dir string, verbose bool) error {
	for _, m := range mkdir {
		args := []string{"-p"}
		if m.Mode != "" {
			args = append(args, "-m", m.Mode)
		}
		args = append(args, m.Path)
		if err := runCmd(ctx, exec.Command("mkdir", args...), dir, nil, verbose); err != nil {
			return fmt.Errorf("mkdir %s: %w", m.Path, err)
		}
		if m.Owner != "" {
			if err := runCmd(ctx, exec.Command("chown", m.Owner, m.Path), dir, nil, verbose); err != nil {
				return fmt.Errorf("chown %s: %w", m.Path, err)
			}
		}
	}
	for _, c := range chown {
		args := []string{}
		if c.Recursive {
			args = append(args, "-R")
		}
		owner := c.Owner
		if c.Group != "" {
			owner += ":" + c.Group
		}
		args = append(args, owner, c.Path)
		if err := runCmd(ctx, exec.Command("chown", args...), dir, nil, verbose); err != nil {
			return fmt.Errorf("chown %s: %w", c.Path, err)
		}
	}
	for _, c := range chmod {
		if err := runCmd(ctx, exec.Command("chmod", c.Mode, c.Path), dir, nil, verbose); err != nil {
			return fmt.Errorf("chmod %s: %w", c.Path, err)
		}
	}
	for _, p := range touch {
		if err := runCmd(ctx, exec.Command("touch", p), dir, nil, verbose); err != nil {
			return fmt.Errorf("touch %s: %w", p, err)
		}
	}
	for _, c := range copy {
		if err := runCmd(ctx, exec.Command("cp", c.From, c.To), dir, nil, verbose); err != nil {
			return fmt.Errorf("cp %s %s: %w", c.From, c.To, err)
		}
		if c.Mode != "" {
			if err := runCmd(ctx, exec.Command("chmod", c.Mode, c.To), dir, nil, verbose); err != nil {
				return fmt.Errorf("chmod %s: %w", c.To, err)
			}
		}
	}
	return nil
}

// executeOps runs a list of operations in order.
func (e *Executor) executeOps(ctx context.Context, ops []domain.Operation, dir string, env []string, sctx domain.StepContext, lookup repository.Lookup) error {
	for _, op := range ops {
		switch {
		case op.Raw != "":
			if err := runShellCommands(ctx, []string{repository.Replace(op.Raw, lookup)}, dir, env, sctx.Verbose); err != nil {
				return err
			}
		case op.User != nil:
			if err := e.executeUser(ctx, op.User, sctx.Verbose); err != nil {
				return err
			}
		case len(op.Mkdir) > 0 || len(op.Chown) > 0 || len(op.Chmod) > 0 || len(op.Copy) > 0 || len(op.Touch) > 0:
			if err := e.executeFS(ctx, op.Mkdir, op.Chown, op.Chmod, op.Copy, op.Touch, dir, sctx.Verbose); err != nil {
				return err
			}
		case op.Packages != nil:
			if err := e.executePackages(ctx, op.Packages, sctx); err != nil {
				return err
			}
		case op.SourceInstall != nil:
			if _, _, err := e.executeSourceInstall(ctx, op.SourceInstall, sctx, lookup); err != nil {
				return err
			}
		case op.BinaryInstall != nil:
			if err := e.executeBinary(ctx, op.BinaryInstall, lookup); err != nil {
				return err
			}
		case len(op.Verify) > 0:
			if err := e.executeVerify(op.Verify, lookup); err != nil {
				return err
			}
		case op.Generate != nil:
			if err := e.executeGenerate(ctx, op.Generate, dir, sctx.Verbose); err != nil {
				return err
			}
		}
	}
	return nil
}

// executeGenerate runs a code generator in dir, creating the output dir first.
func (e *Executor) executeGenerate(ctx context.Context, g *domain.GenerateOp, dir string, verbose bool) error {
	if g == nil || g.Tool == "" {
		return fmt.Errorf("generate op has no tool")
	}
	if g.Out != "" {
		outDir := filepath.Join(dir, g.Out)
		if err := os.MkdirAll(outDir, 0o755); err != nil {
			return fmt.Errorf("generate mkdir %s: %w", outDir, err)
		}
	}
	args := []string{}
	args = append(args, g.Flags...)
	if g.Out != "" {
		// protoc-style output directive.
		args = append(args, "--c_out="+g.Out)
	}
	if g.Input != "" {
		args = append(args, g.Input)
	}
	cmd := exec.Command(g.Tool, args...)
	if err := runCmd(ctx, cmd, dir, nil, verbose); err != nil {
		return fmt.Errorf("generate %s: %w", g.Tool, err)
	}
	return nil
}
