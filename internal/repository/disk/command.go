package disk

import (
	"context"
	"os"
	"os/exec"
	"syscall"
)

// runProcess runs a command in its own process group, returning its combined
// output. If ctx is cancelled while the command runs, the entire process group
// (the command and any children it spawned, e.g. `sh -c "sleep 30"`) is
// killed — a bare exec.CommandContext would only kill the direct child.
func runProcess(ctx context.Context, cmd *exec.Cmd) ([]byte, error) {
	setProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		return nil, err
	case <-ctx.Done():
		killGroup(cmd)
		<-done
		return nil, ctx.Err()
	}
}

// runProcessVerbose runs a command in its own process group, streaming
// stdout/stderr to the terminal. Cancellation kills the whole process group.
func runProcessVerbose(ctx context.Context, cmd *exec.Cmd) error {
	setProcessGroup(cmd)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		killGroup(cmd)
		<-done
		return ctx.Err()
	}
}

func setProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// killGroup sends SIGKILL to the process group the command leads. After a
// successful Start with Setpgid, the group id equals the child's pid, so
// signaling -pid reaches the command and every process it spawned.
func killGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
