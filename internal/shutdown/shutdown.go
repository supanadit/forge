// Package shutdown provides signal-aware context management for the CLI app.
package shutdown

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
)

// DefaultSignals are the signals that trigger graceful shutdown.
var DefaultSignals = []os.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP}

// Manager owns a cancellable context tied to OS signals.
type Manager struct {
	ctx    context.Context
	cancel context.CancelFunc
	once   sync.Once
	done   chan os.Signal
}

// New creates a Manager that cancels its context on the given signals.
func New(sig ...os.Signal) *Manager {
	if len(sig) == 0 {
		sig = DefaultSignals
	}
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{
		ctx:    ctx,
		cancel: cancel,
		done:   make(chan os.Signal, 1),
	}
	signal.Notify(m.done, sig...)
	return m
}

// Context returns the manager's cancellable context.
func (m *Manager) Context() context.Context {
	return m.ctx
}

// Wait blocks until a signal is received, cancels the context, and returns it.
func (m *Manager) Wait() os.Signal {
	sig := <-m.done
	m.once.Do(func() { m.cancel() })
	return sig
}

// Stop stops signal delivery and cancels the context.
func (m *Manager) Stop() {
	signal.Stop(m.done)
	m.once.Do(func() { m.cancel() })
}

// PrintInterrupted writes a graceful-shutdown notice to stderr.
func PrintInterrupted(sig os.Signal) {
	fmt.Fprintf(os.Stderr, "\nReceived %s. Shutting down gracefully... (press Ctrl+C again to force)\n", sig)
}
