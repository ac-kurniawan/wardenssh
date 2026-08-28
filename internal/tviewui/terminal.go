package tviewui

import (
	"fmt"
	"os/exec"
	"sync"
	"time"

	"github.com/blacknon/tvxterm"
	"github.com/gdamore/tcell/v2"
	"github.com/mattn/go-runewidth"
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
	key       string
	alias     string
	source    string
	host      string
	started   time.Time
	viewTitle string
	view      tview.Primitive
	backend   *PtyBackend
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

	mu           sync.Mutex
	sessions     map[string]*terminalSession
	order        []string // insertion order, for "most recent" selection
	active       string   // key of the displayed session
	testRunning  bool
	titleFocused bool // last SetSessionTitleState value (for the uptime ticker)

	titleTicker *time.Ticker
	stopTitle   chan struct{}
	tickerOn    bool
}

// NewTerminalPane creates the terminal pane. The app reference is used for
// redraw scheduling (tvxterm needs it). May be nil for tests that don't draw.
func NewTerminalPane(app *tview.Application) *TerminalPane {
	p := &TerminalPane{
		app:      app,
		pages:    tview.NewPages(),
		status:   tview.NewTextView().SetDynamicColors(true).SetWordWrap(true),
		sessions: map[string]*terminalSession{},
	}
	p.status.SetText(emptyTerminalText()).
		SetTextAlign(tview.AlignLeft)
	p.status.SetBorder(true).SetTitle(" Terminal — No Session ")
	p.pages.AddPage("status", p.status, true, false)

	p.flex = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(p.pages, 0, 1, true)
	return p
}

// emptyTerminalText is the teaching empty state shown when no session exists.
// Mirrors index-tui.html's 3-step flow + security note.
func emptyTerminalText() string {
	return "\n" +
		"  [white]No active session[-]\n\n" +
		"  [#64748B]Select a host and press [yellow]Enter[-] to connect.[-]\n" +
		"  [#64748B]Your keys stay in [#22C55E]RAM only[-][#64748B] — never written to disk.[-]\n\n" +
		"  [#38BDF8]Quick start:[-]\n" +
		"    [#A855F7]↑/↓[-] Navigate  [#A855F7]Enter[-] Connect  [#A855F7]/[-] Filter  [#A855F7]Ctrl+N[-] New\n\n" +
		"  [#64748B]Tips: [yellow]Ctrl+B[-] moves focus between panes without closing the session.[-]\n" +
		"  [#64748B]       Background sessions keep running — re-select the host to switch back.[-]"
}

// EmptyStatusText exposes the empty-state text for tests.
func EmptyStatusText() string { return emptyTerminalText() }

// Primitive returns the tview primitive for layout embedding.
func (p *TerminalPane) Primitive() tview.Primitive { return p.flex }

// SetFocused updates the border color of the currently displayed surface (the
// active session view, or the status page when idle) to reflect keyboard
// focus: accent when focused, inactive otherwise.
func (p *TerminalPane) SetFocused(focused bool) {
	style := tcell.StyleDefault.Foreground(InactiveBorder)
	if focused {
		style = tcell.StyleDefault.Foreground(AccentColor)
	}
	p.mu.Lock()
	var view tview.Primitive
	if s, ok := p.sessions[p.active]; ok && s != nil && s.view != nil {
		view = s.view
	}
	p.mu.Unlock()
	if view != nil {
		if b, ok := view.(*terminalView); ok {
			b.SetBorderStyle(style)
		}
		return
	}
	p.status.SetBorderStyle(style)
}

// BorderColor returns the border color of the currently displayed surface
// (used in tests).
func (p *TerminalPane) BorderColor() tcell.Color {
	p.mu.Lock()
	var view tview.Primitive
	if s, ok := p.sessions[p.active]; ok && s != nil && s.view != nil {
		view = s.view
	}
	p.mu.Unlock()
	if view != nil {
		if b, ok := view.(*terminalView); ok {
			return b.GetBorderColor()
		}
	}
	return p.status.GetBorderColor()
}

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

// OrderedKeys returns the ordered session keys that are still live (for tab bar).
func (p *TerminalPane) OrderedKeys() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []string
	for _, k := range p.order {
		if _, ok := p.sessions[k]; ok {
			out = append(out, k)
		}
	}
	return out
}

// EntryForKey returns alias/source for a given session key (for tab bar labels).
func (p *TerminalPane) EntryForKey(key string) (alias, source string, ok bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	s, ok := p.sessions[key]
	if s == nil || !ok {
		return "", "", false
	}
	return s.alias, s.source, true
}

// SetSessionForTest registers a session for the given key without a real
// backend (tests only), making it the active session.
func (p *TerminalPane) SetSessionForTest(key, alias, source string) {
	p.mu.Lock()
	p.sessions[key] = &terminalSession{
		key: key, alias: alias, source: source,
		host: "1.2.3.4", started: time.Now(),
	}
	p.order = append(p.order, key)
	p.active = key
	p.mu.Unlock()
	p.setSessionTitle(key, false)
}

// FormatSessionTitleForTest exposes the session-title formatter (tests only).
func FormatSessionTitleForTest(alias, host, state string, up time.Duration) string {
	return formatSessionTitle(alias, host, state, up)
}

// FormatSessionTitleWithSourceForTest exposes the source-aware formatter (tests only).
func FormatSessionTitleWithSourceForTest(alias, host, source, state string, up time.Duration) string {
	return formatSessionTitleWithSource(alias, host, source, state, up)
}

// formatSessionTitle composes the terminal pane title: "💻 alias (host)
// [CONNECTED/ACTIVE SESSION] · Up: <elapsed>". The host is tail-truncated so a
// long address never clips the state badge.
func formatSessionTitle(alias, host, state string, up time.Duration) string {
	return formatSessionTitleWithSource(alias, host, "file", state, up)
}

// formatSessionTitleWithSource adds a RAM-only agent hint for vault-sourced
// sessions so the zero-disk guarantee is surfaced in the title chrome (mirrors
// index-tui.html "Agent: ed25519 • RAM only").
func formatSessionTitleWithSource(alias, host, source, state string, up time.Duration) string {
	addr := host
	if runewidth.StringWidth(addr) > 30 {
		addr = runewidth.Truncate(addr, 30, "…")
	}
	upStr := formatUptime(up)
	base := fmt.Sprintf("💻 %s (%s) [%s] · Up: %s", alias, addr, state, upStr)
	if source != "file" && source != "" {
		base += " • Agent: RAM-only"
	}
	return base
}

// formatUptime renders a duration compactly: "0s", "1m30s", "14d 6h".
func formatUptime(up time.Duration) string {
	total := int64(up.Seconds())
	if total < 0 {
		total = 0
	}
	days := total / 86400
	hours := (total % 86400) / 3600
	mins := (total % 3600) / 60
	secs := total % 60
	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh", days, hours)
	case hours > 0:
		return fmt.Sprintf("%dh%dm", hours, mins)
	case mins > 0:
		return fmt.Sprintf("%dm%ds", mins, secs)
	default:
		return fmt.Sprintf("%ds", secs)
	}
}

// ActiveTitle returns the title of the displayed session (used in tests).
func (p *TerminalPane) ActiveTitle() string {
	p.mu.Lock()
	var title string
	if s, ok := p.sessions[p.active]; ok {
		title = s.viewTitle
	}
	p.mu.Unlock()
	return title
}

// ActiveUptime returns the elapsed time since the active session started (or 0).
func (p *TerminalPane) ActiveUptime() time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()
	s := p.sessions[p.active]
	if s == nil || s.started.IsZero() {
		return 0
	}
	return time.Since(s.started)
}

// SetSessionStartForTest rewinds a session's start time (tests only), so the
// uptime telemetry can be exercised deterministically.
func (p *TerminalPane) SetSessionStartForTest(key string, started time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if s, ok := p.sessions[key]; ok {
		s.started = started
	}
}

// SetSessionTitleState updates the displayed session's title state badge:
// "ACTIVE SESSION" when the terminal has focus, "CONNECTED" otherwise. The
// focused flag is remembered so the uptime ticker can refresh the title.
func (p *TerminalPane) SetSessionTitleState(focused bool) {
	p.mu.Lock()
	p.titleFocused = focused
	active := p.active
	s := p.sessions[active]
	p.mu.Unlock()
	if s == nil {
		return
	}
	p.setSessionTitle(active, focused)
}

// RefreshActiveTitle recomputes the displayed session's title from the current
// time, so "Up:" telemetry keeps ticking instead of freezing at session start.
// Called by the 1s title ticker and directly by tests.
func (p *TerminalPane) RefreshActiveTitle() {
	p.mu.Lock()
	active := p.active
	focused := p.titleFocused
	s := p.sessions[active]
	p.mu.Unlock()
	if s == nil {
		return
	}
	p.setSessionTitle(active, focused)
}

// setSessionTitle writes the mockup title onto a session's terminal view.
// Callers may hold p.mu; viewTitle is updated under the lock.
func (p *TerminalPane) setSessionTitle(key string, focused bool) {
	p.mu.Lock()
	s := p.sessions[key]
	if s == nil {
		p.mu.Unlock()
		return
	}
	state := "CONNECTED"
	if focused {
		state = "ACTIVE SESSION"
	}
	up := time.Duration(0)
	if s.started.UnixNano() != 0 {
		up = time.Now().Sub(s.started)
	}
	s.viewTitle = formatSessionTitleWithSource(s.alias, s.host, s.source, state, up)
	title := s.viewTitle
	view := s.view
	p.mu.Unlock()

	if b, ok := view.(*terminalView); ok {
		b.SetTitle(" " + title + " ")
	}
}

// SetSessionViewForTest attaches a terminal view to an already registered
// session (tests only; mirrors SetSessionForTest).
func (p *TerminalPane) SetSessionViewForTest(key string, view tview.Primitive) {
	p.mu.Lock()
	if s, ok := p.sessions[key]; ok {
		s.view = view
	}
	p.mu.Unlock()
}

// CopyActiveSelection copies the displayed session's text selection to the
// clipboard and clears it, reporting whether a selection was present. Called
// from the UI thread (e.g. Ctrl+C in the terminal pane).
func (p *TerminalPane) CopyActiveSelection() bool {
	p.mu.Lock()
	s := p.sessions[p.active]
	p.mu.Unlock()
	if s == nil {
		return false
	}
	tv, ok := s.view.(*terminalView)
	if !ok {
		return false
	}
	return tv.CopySelection()
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
	started := time.Now()

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
		host: entry.HostName, started: started,
		view: term, backend: backend,
	}
	p.order = append(p.order, key)
	p.active = key
	p.mu.Unlock()

	p.setSessionTitle(key, false)

	p.pages.AddPage(pageName(key), term, true, true)
	p.pages.SwitchToPage(pageName(key))
	p.startTitleTicker()
	return nil
}

// startTitleTicker launches (idempotent) a 1-second goroutine that refreshes
// the active session's title, so the "Up:" uptime telemetry keeps ticking
// while a session runs. Redraws are marshaled through the app (nil in tests).
func (p *TerminalPane) startTitleTicker() {
	p.mu.Lock()
	if p.tickerOn {
		p.mu.Unlock()
		return
	}
	stop := make(chan struct{})
	p.stopTitle = stop
	p.titleTicker = time.NewTicker(1 * time.Second)
	p.tickerOn = true
	p.mu.Unlock()

	go func() {
		for {
			select {
			case <-p.titleTicker.C:
				p.mu.Lock()
				hasSession := len(p.sessions) > 0
				p.mu.Unlock()
				if !hasSession {
					continue
				}
				p.RefreshActiveTitle()
				if p.app != nil {
					p.app.QueueUpdateDraw(func() {})
				}
			case <-stop:
				return
			}
		}
	}()
}

// stopTitleTicker stops the uptime ticker goroutine (idempotent).
func (p *TerminalPane) stopTitleTicker() {
	p.mu.Lock()
	p.tickerOn = false
	if p.titleTicker != nil {
		p.titleTicker.Stop()
		p.titleTicker = nil
	}
	if p.stopTitle != nil {
		close(p.stopTitle)
		p.stopTitle = nil
	}
	p.mu.Unlock()
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
	p.stopTitleTicker()
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
