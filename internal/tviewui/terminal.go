package tviewui

import (
	"fmt"
	"os/exec"
	"sync"

	"github.com/blacknon/tvxterm"
	"github.com/rivo/tview"

	"github.com/ac-kurniawan/wardenssh/internal/hosts"
	"github.com/ac-kurniawan/wardenssh/internal/session"
)

// SessionKey uniquely identifies a host session across sources (Q11/C
// no-dedup: the same alias may exist under "file" and "vw:name").
func SessionKey(alias, source string) string {
	return alias + "\x00" + source
}

// terminalSession is one running ssh session with its own terminal view + PTY.
type terminalSession struct {
	key     string
	alias   string
	source  string
	view    tview.Primitive
	backend *PtyBackend
}

// TerminalPane is the right pane: a tview.Pages hosting one tvxterm.View per
// concurrent session (Q18/iii yield-and-switch, N sessions). Only the active
// session's view is shown, but background sessions keep running and draining.
// A "status" page is shown when no session exists.
type TerminalPane struct {
	app    *tview.Application
	flex   *tview.Flex
	pages  *tview.Pages
	status *tview.TextView

	mu          sync.Mutex
	sessions    map[string]*terminalSession
	order       []string // insertion order, for "most recent" selection
	active      string   // key of the displayed session
	testRunning bool
}

// NewTerminalPane creates the terminal pane. The app reference is used for
// redraw scheduling (tvxterm needs it). May be nil for tests that don't draw.
func NewTerminalPane(app *tview.Application) *TerminalPane {
	p := &TerminalPane{
		app:      app,
		pages:    tview.NewPages(),
		status:   tview.NewTextView(),
		sessions: map[string]*terminalSession{},
	}
	p.status.SetText(" [yellow]No active session[-]").
		SetTextAlign(tview.AlignLeft)
	p.status.SetBorder(true).SetTitle(" Terminal ")
	p.pages.AddPage("status", p.status, true, false)

	p.flex = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(p.pages, 0, 1, true)
	return p
}

// Primitive returns the tview primitive for layout embedding.
func (p *TerminalPane) Primitive() tview.Primitive { return p.flex }

// IsRunning reports whether any terminal session is running.
func (p *TerminalPane) IsRunning() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.testRunning || len(p.sessions) > 0
}

// SetRunningForTest overrides the running flag without starting a session
// (tests only). The app's pane-focus routing is tested without spawning ssh.
func (p *TerminalPane) SetRunningForTest(v bool) {
	p.mu.Lock()
	p.testRunning = v
	p.mu.Unlock()
}

// SessionCount returns the number of live sessions.
func (p *TerminalPane) SessionCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.sessions)
}

// HasSession reports whether a session with the given key is running.
func (p *TerminalPane) HasSession(key string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, ok := p.sessions[key]
	return ok
}

// ActiveEntry returns the alias/source of the displayed (active) session.
func (p *TerminalPane) ActiveEntry() (alias, source string, ok bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	s := p.sessions[p.active]
	if s == nil {
		return "", "", false
	}
	return s.alias, s.source, true
}

// SetSessionForTest registers a session for the given key without a real
// backend (tests only), making it the active session.
func (p *TerminalPane) SetSessionForTest(key, alias, source string) {
	p.mu.Lock()
	p.sessions[key] = &terminalSession{key: key, alias: alias, source: source}
	p.order = append(p.order, key)
	p.active = key
	p.mu.Unlock()
}

// StartSSH builds an ssh exec.Cmd from argv+env and starts a new session. The
// onExit callback is invoked on the backend read-loop goroutine when the ssh
// process exits; UI updates must be marshaled (QueueUpdateDraw) by the caller.
// A previous session, if any, keeps running in the background.
func (p *TerminalPane) StartSSH(entry hosts.Entry, argv []string, env []string, onExit func(error)) error {
	if len(argv) == 0 {
		return fmt.Errorf("terminal: empty argv")
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Env = session.MergeEnv(env)
	return p.StartSSHFromCmd(entry, cmd, env, onExit)
}

// StartSSHFromCmd starts a new terminal session with a pre-built exec.Cmd.
// Used by StartSSH and by tests.
func (p *TerminalPane) StartSSHFromCmd(entry hosts.Entry, cmd *exec.Cmd, env []string, onExit func(error)) error {
	key := SessionKey(entry.Alias, entry.Source)

	p.mu.Lock()
	if _, exists := p.sessions[key]; exists {
		p.mu.Unlock()
		return fmt.Errorf("terminal: a session for %q is already running", entry.Alias)
	}
	p.mu.Unlock()

	term := newTerminalView(p.app, entry.Alias)

	backend, err := NewPtyBackend(cmd, 80, 24)
	if err != nil {
		return fmt.Errorf("terminal: create backend: %w", err)
	}

	term.SetBackendExitHandler(func(_ *tvxterm.View, exitErr error) {
		// Runs on the backend read-loop goroutine — only touch mutex-protected
		// state here; UI updates happen in App.onExit via QueueUpdateDraw.
		p.mu.Lock()
		if _, ok := p.sessions[key]; ok {
			delete(p.sessions, key)
			if p.active == key {
				p.active = p.mostRecentUnlocked()
			}
		}
		p.mu.Unlock()
		_ = backend.Close()
		if onExit != nil {
			onExit(exitErr)
		}
	})

	term.Attach(backend)

	p.mu.Lock()
	p.sessions[key] = &terminalSession{
		key: key, alias: entry.Alias, source: entry.Source,
		view: term, backend: backend,
	}
	p.order = append(p.order, key)
	p.active = key
	p.mu.Unlock()

	p.pages.AddPage(pageName(key), term, true, true)
	p.pages.SwitchToPage(pageName(key))
	return nil
}

// Activate makes the given session the displayed one (yield-and-switch: select
// a host whose session runs in the background -> show it; no duplicate spawn).
func (p *TerminalPane) Activate(key string) {
	p.mu.Lock()
	if _, ok := p.sessions[key]; !ok {
		p.mu.Unlock()
		return
	}
	p.active = key
	p.mu.Unlock()
	p.pages.SwitchToPage(pageName(key))
}

// CloseSession terminates one session and its PTY. If it was the displayed
// session, the most recently started remaining session becomes active; if none
// remain, the status page is shown.
func (p *TerminalPane) CloseSession(key string) {
	p.mu.Lock()
	s, ok := p.sessions[key]
	if ok {
		delete(p.sessions, key)
		if p.active == key {
			p.active = p.mostRecentUnlocked()
		}
	}
	p.mu.Unlock()

	if ok && s != nil && s.backend != nil {
		_ = s.backend.Close()
	}
	if s != nil && s.view != nil {
		p.pages.RemovePage(pageName(key))
	}
	p.syncPages()
}

// SyncToMostRecent switches the displayed page to the most recently started
// live session, or the status page when none remain. Safe to call after a
// background session exits (from the UI thread).
func (p *TerminalPane) SyncToMostRecent() {
	p.mu.Lock()
	if p.active == "" || p.sessions[p.active] == nil {
		p.active = p.mostRecentUnlocked()
	}
	p.mu.Unlock()
	p.syncPages()
}

// syncPages shows either the active session's page or the status page.
func (p *TerminalPane) syncPages() {
	p.mu.Lock()
	active := p.active
	_, ok := p.sessions[active]
	p.mu.Unlock()
	if ok {
		p.pages.SwitchToPage(pageName(active))
	} else {
		p.pages.SwitchToPage("status")
	}
}

// mostRecentUnlocked returns the last-started key still in the sessions map.
// Callers must hold p.mu.
func (p *TerminalPane) mostRecentUnlocked() string {
	for i := len(p.order) - 1; i >= 0; i-- {
		key := p.order[i]
		if _, ok := p.sessions[key]; ok {
			return key
		}
	}
	return ""
}

// Close terminates all sessions and resets the pane to the status page.
func (p *TerminalPane) Close() {
	p.mu.Lock()
	var backends []*PtyBackend
	for _, s := range p.sessions {
		if s != nil && s.backend != nil {
			backends = append(backends, s.backend)
		}
	}
	p.sessions = map[string]*terminalSession{}
	p.order = nil
	p.active = ""
	p.testRunning = false
	p.mu.Unlock()

	for _, b := range backends {
		_ = b.Close()
	}
	// Reset pages to the status page only.
	p.pages.RemovePage("status")
	p.pages.AddPage("status", p.status, true, true)
	p.pages.SwitchToPage("status")
}

func pageName(key string) string {
	return "sess:" + key
}
