package tviewui

import (
	"fmt"
	"os"
	"os/exec"
	"sync"

	"github.com/blacknon/tvxterm"
	"github.com/rivo/tview"

	"github.com/ac-kurniawan/wardenssh/internal/hosts"
)

// TerminalPane is the right pane: a tvxterm.View terminal emulator that runs
// ssh (or any command) inside a PTY. When the command exits, the onExit
// callback fires so the app can return to the host list.
type TerminalPane struct {
	app     *tview.Application
	view    *tvxterm.View
	backend *PtyBackend
	flex    *tview.Flex
	status  *tview.TextView

	mu      sync.Mutex
	running bool
}

// NewTerminalPane creates the terminal pane. The app reference is used for
// redraw scheduling (tvxterm needs it). May be nil for tests that don't draw.
func NewTerminalPane(app *tview.Application) *TerminalPane {
	p := &TerminalPane{
		app:    app,
		view:   nil,
		status: tview.NewTextView(),
	}
	p.status.SetText(" [yellow]No active session[-]").
		SetTextAlign(tview.AlignLeft)
	p.status.SetBorder(true).SetTitle(" Terminal ")

	p.flex = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(p.status, 0, 1, false)
	return p
}

// Primitive returns the tview primitive for layout embedding.
func (p *TerminalPane) Primitive() tview.Primitive { return p.flex }

// IsRunning reports whether a terminal session is currently active.
func (p *TerminalPane) IsRunning() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.running
}

// StartSSH builds an ssh exec.Cmd from argv+env and starts the terminal.
// The onExit callback is invoked when the ssh process exits.
func (p *TerminalPane) StartSSH(entry hosts.Entry, argv []string, env []string, onExit func(error)) error {
	if len(argv) == 0 {
		return fmt.Errorf("terminal: empty argv")
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Env = append(os.Environ(), env...)
	return p.StartSSHFromCmd(entry, cmd, env, onExit)
}

// StartSSHFromCmd starts the terminal with a pre-built exec.Cmd. Used by
// StartSSH and by tests.
func (p *TerminalPane) StartSSHFromCmd(entry hosts.Entry, cmd *exec.Cmd, env []string, onExit func(error)) error {
	p.mu.Lock()
	if p.running {
		p.mu.Unlock()
		return fmt.Errorf("terminal: a session is already running")
	}
	p.mu.Unlock()

	term := tvxterm.New(p.app)
	term.SetBorder(true).SetTitle(fmt.Sprintf(" %s ", entry.Alias))

	backend, err := NewPtyBackend(cmd, 80, 24)
	if err != nil {
		return fmt.Errorf("terminal: create backend: %w", err)
	}

	term.SetBackendExitHandler(func(_ *tvxterm.View, exitErr error) {
		p.mu.Lock()
		p.running = false
		p.mu.Unlock()
		_ = backend.Close()
		if onExit != nil {
			onExit(exitErr)
		}
	})

	term.Attach(backend)

	p.mu.Lock()
	p.running = true
	p.mu.Unlock()

	p.view = term
	p.backend = backend
	p.flex.Clear()
	p.flex.AddItem(term, 0, 1, true)

	return nil
}

// Close stops the current terminal session (if any) and resets the pane.
func (p *TerminalPane) Close() {
	p.mu.Lock()
	p.running = false
	p.mu.Unlock()
	if p.backend != nil {
		_ = p.backend.Close()
	}
	p.flex.Clear()
	p.flex.AddItem(p.status, 0, 1, false)
	p.view = nil
	p.backend = nil
}
