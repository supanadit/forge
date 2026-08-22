package disk

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRunProcess_CancelReturnsCtxErr verifies that cancelling the context
// causes runProcess to return ctx.Err().
func TestRunProcess_CancelReturnsCtxErr(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.Command("sleep", "30")
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err := runProcess(ctx, cmd)
	assert.ErrorIs(t, err, context.Canceled)
}

// TestRunProcess_KillsGrandchildProcess verifies that cancelling the context
// kills the whole process group — including a grandchild spawned by the shell
// — leaving no orphaned processes.
func TestRunProcess_KillsGrandchildProcess(t *testing.T) {
	// Spawn a shell that runs a long-lived grandchild `sleep` with a unique
	// argument so we can detect any leaked process afterwards.
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.Command("sh", "-c", "sleep 300")
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err := runProcess(ctx, cmd)
	assert.ErrorIs(t, err, context.Canceled)

	// Wait briefly for the OS to reap/clean up.
	time.Sleep(200 * time.Millisecond)

	// No `sleep 300` process may remain (the grandchild of the killed shell).
	// The [s]leep bracket avoids matching the grep command's own line.
	out, err := exec.Command("sh", "-c", `ps -eo cmd | grep -c "[s]leep 300" || true`).Output()
	require.NoError(t, err)
	assert.Equal(t, "0", strings.TrimSpace(string(out)), "expected no leaked sleep process")
}

// TestRunProcess_CompletesNormally verifies a short-lived command returns its
// exit error (nil on success) without waiting for cancellation.
func TestRunProcess_CompletesNormally(t *testing.T) {
	ctx := context.Background()
	cmd := exec.Command("echo", "hi")
	_, err := runProcess(ctx, cmd)
	assert.NoError(t, err)
}
