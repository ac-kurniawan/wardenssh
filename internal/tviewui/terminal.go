package tviewui

import (
	"fmt"
	"os/exec"
	"sync"
	"time"

	tvxterm "github.com/ac-kurniawan/wardenssh/third_party/tvxterm"
	"github.com/gdamore/tcell/v2"
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
	app         *tview.Application
	flex        *tview.Flex
	pages       *tview.Pages
	status      *tview.TextView
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
func (p *TerminalPane) Primitive() tview.Primitive { return p }

// activeView returns the terminal view currently on screen, or nil when the
// status page is showing or the view is not a terminal.
func (p *TerminalPane) activeView() *terminalView {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	s := p.sessions[p.active]
	if s == nil {
		return nil
	}
	view, _ := s.view.(*terminalView)
	return view
}

// MouseHandler returns the terminal view as the mouse capture for the whole
// selection drag. tview routes every later event to that primitive, so a drag
// that leaves the pane, and a wheel tick while the button is held, both still
// reach the view. The Flex and Pages underneath drop those events.
func (p *TerminalPane) MouseHandler() func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(p tview.Primitive)) (bool, tview.Primitive) {
	return func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(p tview.Primitive)) (bool, tview.Primitive) {
		if view := p.activeView(); view != nil && view.Dragging() {
			consumed, _ := view.MouseHandler()(action, event, setFocus)
			if view.Dragging() {
				return true, view
			}
			return consumed, nil
		}
		consumed, capture := p.flex.MouseHandler()(action, event, setFocus)
		if view := p.activeView(); view != nil && view.Dragging() {
			return true, view
		}
		return consumed, capture
	}
}

func (p *TerminalPane) Draw(screen tcell.Screen) { p.flex.Draw(screen) }

func (p *TerminalPane) SetRect(x, y, width, height int) { p.flex.SetRect(x, y, width, height) }

func (p *TerminalPane) GetRect() (int, int, int, int) { return p.flex.GetRect() }

func (p *TerminalPane) InputHandler() func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
	return p.flex.InputHandler()
}

func (p *TerminalPane) Focus(delegate func(p tview.Primitive)) { p.flex.Focus(delegate) }

func (p *TerminalPane) HasFocus() bool { return p.flex.HasFocus() }

func (p *TerminalPane) Blur() { p.flex.Blur() }

func (p *TerminalPane) PasteHandler() func(text string, setFocus func(p tview.Primitive)) {
	return p.flex.PasteHandler()
}

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
		key: key, alias: alias, source: source, started: time.Now(),
	}
	p.order = append(p.order, key)
	p.active = key
	p.mu.Unlock()
	p.setSessionTitle(key)
}

// FormatSessionTitleForTest exposes the session-title formatter (tests only).
func FormatSessionTitleForTest(alias string) string {
	return formatSessionTitle(alias)
}

// formatSessionTitle composes the terminal pane's border title: the session
// identity alone. State, uptime, ping and the agent badge belong to the
// SessionHeader row above the terminal — repeating them here is what made the
// title overflow into the tab bar on narrow panes.
func formatSessionTitle(alias string) string {
	return "💻 " + alias
}

// ActiveTitle returns the title of the displayed session, including the scroll
// marker when the view is scrolled up (used in tests).
func (p *TerminalPane) ActiveTitle() string {
	p.mu.Lock()
	var title string
	var view tview.Primitive
	if s, ok := p.sessions[p.active]; ok {
		title = s.viewTitle
		view = s.view
	}
	p.mu.Unlock()
	if b, ok := view.(*terminalView); ok {
		return b.TerminalTitle()
	}
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

// setSessionTitle writes the session's identity onto its terminal view's border
// title. Callers may hold p.mu; viewTitle is updated under the lock.
//
// The title carries identity only: state, uptime, ping and the agent badge live
// on the SessionHeader row, and repeating them here is what ran the title into
// the tab bar on narrow panes.
func (p *TerminalPane) setSessionTitle(key string) {
	p.mu.Lock()
	s := p.sessions[key]
	if s == nil {
		p.mu.Unlock()
		return
	}
	title := formatSessionTitle(s.alias)
	s.viewTitle = title
	view := s.view
	p.mu.Unlock()

	if b, ok := view.(*terminalView); ok {
		// The view owns the scroll marker: it appends one when scrolled up and
		// restores the plain title at the bottom, so a chrome rewrite here
		// cannot silently drop it.
		b.SetTerminalTitle(title)
	}
}

// ScrollableTerminal exposes the scrollback operations of the displayed
// session's view, so callers outside the package can drive and inspect pane
// history (tests, smoke runs). It returns nil when no session is displayed.
type ScrollableTerminal interface {
	ScrollbackUp(lines int)
	ScrollbackDown(lines int)
	ScrollbackPageUp()
	ScrollbackPageDown()
	ScrollbackTop()
	ScrollbackBottom()
	ScrollbackStatus() (offset, rows int)
	ScrollbackText() string
	SendKey(event *tcell.EventKey) bool
}

// ActiveViewForTest returns the displayed session's scrollable view (tests).
func (p *TerminalPane) ActiveViewForTest() ScrollableTerminal {
	p.mu.Lock()
	s := p.sessions[p.active]
	p.mu.Unlock()
	if s == nil {
		return nil
	}
	tv, _ := s.view.(*terminalView)
	if tv == nil {
		return nil
	}
	return tv
}

// SetSessionViewForTest attaches a terminal view to an already registered
// session (tests only; mirrors SetSessionForTest).
func (p *TerminalPane) SetSessionViewForTest(key string, view tview.Primitive) {
	p.mu.Lock()
	if s, ok := p.sessions[key]; ok {
		s.view = view
	}
	p.mu.Unlock()
	p.pages.AddPage(pageName(key), view, true, true)
	p.pages.SwitchToPage(pageName(key))
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
		started: started, view: term, backend: backend,
	}
	p.order = append(p.order, key)
	p.active = key
	p.mu.Unlock()

	p.setSessionTitle(key)

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
	// The newly displayed view carries the identity title; re-applying it is
	// what keeps the scroll marker composed on top of the right session.
	p.setSessionTitle(key)
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

// CycleSession activates the session at offset from the current active one
// (offset +1 = next, -1 = previous), wrapping around. No-op with <2 sessions.
func (p *TerminalPane) CycleSession(offset int) {
	p.mu.Lock()
	n := len(p.order)
	if n < 2 {
		p.mu.Unlock()
		return
	}
	// Find current position in order.
	idx := -1
	for i, k := range p.order {
		if k == p.active {
			idx = i
			break
		}
	}
	if idx == -1 {
		p.mu.Unlock()
		return
	}
	next := ((idx+offset)%n + n) % n
	p.active = p.order[next]
	key := p.active
	p.mu.Unlock()
	p.pages.SwitchToPage(pageName(key))
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
