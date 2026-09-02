package pkgmgr

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

// runVerbose runs cmd streaming output to the terminal while retaining a tail
// for error context. Cancellation kills the command's whole process group.
func runVerbose(ctx context.Context, cmd *exec.Cmd) ([]byte, error) {
	setProcessGroup(cmd)
	tw := newTailWriter(50)
	cmd.Stdout = io.MultiWriter(os.Stdout, tw)
	cmd.Stderr = io.MultiWriter(os.Stderr, tw)
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

// runQuiet runs cmd capturing its combined output, killing the process group
// on cancellation.
func runQuiet(ctx context.Context, cmd *exec.Cmd) ([]byte, error) {
	setProcessGroup(cmd)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return buf.Bytes(), err
	case <-ctx.Done():
		killGroup(cmd)
		<-done
		return buf.Bytes(), ctx.Err()
	}
}

func setProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

func killGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}

// tailWriter keeps the last maxLines complete lines plus any partial line.
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
			t.partial.WriteString(line)
			break
		}
		t.lines = append(t.lines, strings.TrimSuffix(line, "\n"))
		if len(t.lines) > t.maxLines {
			t.lines = t.lines[len(t.lines)-t.maxLines:]
		}
	}
	return len(p), nil
}

func (t *tailWriter) Bytes() []byte {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.partial.Len() == 0 {
		return []byte(strings.Join(t.lines, "\n"))
	}
	return []byte(strings.Join(append(t.lines, t.partial.String()), "\n"))
}