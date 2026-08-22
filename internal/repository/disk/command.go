package disk

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
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
// stdout/stderr to the terminal (or the supplied writers) while retaining the
// last maxTailLines lines so a failure can be reported with context.
// Cancellation kills the whole process group.
func runProcessVerbose(ctx context.Context, cmd *exec.Cmd, stdout, stderr io.Writer) ([]byte, error) {
	setProcessGroup(cmd)
	tw := newTailWriter(50)
	if stdout == nil {
		stdout = os.Stdout
	}
	if stderr == nil {
		stderr = os.Stderr
	}
	cmd.Stdout = io.MultiWriter(stdout, tw)
	cmd.Stderr = io.MultiWriter(stderr, tw)
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		return tw.Bytes(), err
	case <-ctx.Done():
		killGroup(cmd)
		<-done
		return tw.Bytes(), ctx.Err()
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

// tailWriter is an io.Writer that keeps the last maxLines complete lines plus
// any trailing partial line. It is safe for concurrent use (stdout and stderr
// of the same process may write from different goroutines).
type tailWriter struct {
	mu       sync.Mutex
	maxLines int
	lines    []string
	partial  bytes.Buffer
}

func newTailWriter(maxLines int) *tailWriter {
	return &tailWriter{maxLines: maxLines}
}

func (t *tailWriter) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.partial.Write(p)
	for {
		line, err := t.partial.ReadString('\n')
		if len(line) == 0 && err == io.EOF {
			break
		}
		if err == io.EOF {
			// Trailing partial line with no newline yet; keep it in the buffer.
			t.partial.WriteString(line)
			break
		}
		// Complete line (includes the trailing '\n').
		t.lines = append(t.lines, strings.TrimSuffix(line, "\n"))
		if len(t.lines) > t.maxLines {
			t.lines = t.lines[len(t.lines)-t.maxLines:]
		}
	}
	return len(p), nil
}

// Bytes returns the retained tail as a byte slice.
func (t *tailWriter) Bytes() []byte {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.partial.Len() == 0 {
		return []byte(strings.Join(t.lines, "\n"))
	}
	return []byte(strings.Join(append(t.lines, t.partial.String()), "\n"))
}
