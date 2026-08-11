// Package tviewui implements the WardenSSH launcher TUI using tview + tcell
// + tvxterm (embedded terminal emulator). It replaces the earlier Bubble Tea
// TUI (internal/tui) with a split-pane layout: host list on the left, live SSH
// terminal on the right.
package tviewui

import (
	"os"
	"os/exec"
	"sync"

	pty "github.com/aymanbagabas/go-pty"
)

// PtyBackend adapts github.com/aymanbagabas/go-pty (cross-platform: ConPTY on
// Windows, unix pty on *nix) to the tvxterm.Backend interface
// (Read/Write/Resize/Close). This replaces tvxterm's built-in PTYBackend which
// uses creack/pty (Unix-only).
type PtyBackend struct {
	pty  pty.Pty
	cmd  *pty.Cmd
	once sync.Once
}

// NewPtyBackend starts cmd under a cross-platform PTY with the given initial
// size and returns a Backend implementation for tvxterm.View.Attach.
func NewPtyBackend(cmd *exec.Cmd, cols, rows int) (*PtyBackend, error) {
	p, err := pty.New()
	if err != nil {
		return nil, err
	}
	c := p.Command(cmd.Path, cmd.Args[1:]...)
	c.Env = cmd.Env
	if err := c.Start(); err != nil {
		_ = p.Close()
		return nil, err
	}
	if err := p.Resize(cols, rows); err != nil {
		// Resize failure is non-fatal — the terminal will resize on first Draw.
		_ = os.Stderr
	}

	b := &PtyBackend{pty: p, cmd: c}

	// ConPTY on Windows (and some unix PTYs) leaves the PTY master open after
	// process exit. Waiting for the process in a background goroutine and closing
	// the PTY ensures Read() receives EOF/error when the process exits.
	go func() {
		_ = c.Wait()
		_ = b.Close()
	}()

	return b, nil
}

// Read reads output from the PTY master (the child's stdout/stderr).
func (b *PtyBackend) Read(p []byte) (int, error) {
	return b.pty.Read(p)
}

// Write sends input to the PTY master (the child's stdin).
func (b *PtyBackend) Write(p []byte) (int, error) {
	return b.pty.Write(p)
}

// Resize updates the PTY window size. Called by tvxterm.View on each Draw when
// the widget dimensions change.
func (b *PtyBackend) Resize(cols, rows int) error {
	return b.pty.Resize(cols, rows)
}

// Close kills the child process and closes the PTY master. Safe to call
// multiple times (uses sync.Once).
func (b *PtyBackend) Close() error {
	var err error
	b.once.Do(func() {
		if b.cmd != nil && b.cmd.Process != nil {
			_ = b.cmd.Process.Kill()
			_, _ = b.cmd.Process.Wait()
		}
		err = b.pty.Close()
	})
	return err
}
