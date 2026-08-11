// Package tui is the WardenSSH launcher Bubble Tea program (Q10/C host list,
// Q29/B fuzzy filter + scope cycle, Q18/iii live-session green dots, Q31/C
// quit-confirmation modal intercepting SIGINT). The model owns a hosts.List
// and translates keystrokes into list/filter/scope state and connect/quit
// intents; the real session-manager wiring (PTY, suspend-and-exec) plugs in
// via ConnectMsg (Subsystem: PTY session manager, tested separately).
package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/ac-kurniawan/wardenssh/internal/hosts"
)

// ConnectMsg is emitted by Enter on a selected entry, carrying the entry the
// session manager should open. The model does not itself spawn ssh.
type ConnectMsg struct {
	Entry hosts.Entry
}

// Connect intent — handled by the session manager, returning nil.
func newConnectCmd(e hosts.Entry) tea.Cmd {
	return func() tea.Msg { return ConnectMsg{Entry: e} }
}

type state int

const (
	stateList state = iota
	stateQuitModal
)

// Model is the Bubble Tea model for the launcher. It is a value type with a
// pointer-to-List so filter/scope mutations are visible across copies.
type Model struct {
	hostList   *hosts.List
	filter     string
	cursor     int
	st         state
	lastAction string
}

// New returns the initial launcher model over the given host list.
func New(h *hosts.List) Model {
	return Model{hostList: h, st: stateList}
}

// Init satisfies tea.Model.
func (m Model) Init() tea.Cmd { return nil }

// Update satisfies tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch m.st {
		case stateList:
			return m.updateList(msg)
		case stateQuitModal:
			return m.updateQuitModal(msg)
		}
	}
	return m, nil
}

// View satisfies tea.Model. The real rendering (Lip Gloss styling + badges +
// green dots) is a future commit; this minimal view is enough to wire the
// program and assert state in tests.
func (m Model) View() string {
	if m.st == stateQuitModal {
		return "Quit? [k]ill all / [d]etach / [c]ancel"
	}
	return "host list"
}

// --- accessors used by tests / the future renderer ---

// List returns the underlying host list (for scope, visible entries, etc.).
func (m Model) List() *hosts.List { return m.hostList }

// Filter returns the current filter string.
func (m Model) Filter() string { return m.filter }

// InQuitModal reports whether the quit-confirmation modal is open.
func (m Model) InQuitModal() bool { return m.st == stateQuitModal }

// LastAction returns the last modal choice ("killall"/"detach"/"cancel") for
// inspection/testing; "" before any modal action.
func (m Model) LastAction() string { return m.lastAction }

// --- list state ---

func (m Model) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.Type == tea.KeyCtrlC:
		return m.requestQuit()
	case msg.Type == tea.KeyTab:
		m.hostList.Tab()
		return m, nil
	case msg.Type == tea.KeyEnter:
		vis := m.hostList.Visible()
		if len(vis) == 0 {
			return m, nil
		}
		idx := m.cursor
		if idx < 0 || idx >= len(vis) {
			idx = 0
		}
		return m, newConnectCmd(vis[idx])
	case msg.Type == tea.KeyBackspace:
		if len(m.filter) > 0 {
			m.filter = m.filter[:len(m.filter)-1]
			m.hostList.SetFilter(m.filter)
		}
		return m, nil
	case msg.Type == tea.KeyRunes:
		s := string(msg.Runes)
		if s == "q" {
			return m.requestQuit()
		}
		m.filter += s
		m.hostList.SetFilter(m.filter)
		return m, nil
	}
	return m, nil
}

// requestQuit implements Q31/C: 'q'/Ctrl+C quit immediately when no live
// sessions exist; otherwise open the confirmation modal.
func (m Model) requestQuit() (tea.Model, tea.Cmd) {
	if m.hasLiveSessions() {
		m.st = stateQuitModal
		return m, nil
	}
	m.lastAction = "killall" // no sessions to kill; clean quit
	return m, tea.Quit
}

func (m Model) hasLiveSessions() bool {
	for _, e := range m.hostList.All() {
		if e.Live {
			return true
		}
	}
	return false
}

// --- quit modal state ---

func (m Model) updateQuitModal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.Type == tea.KeyRunes:
		switch string(msg.Runes) {
		case "k", "y":
			m.lastAction = "killall"
			m.clearAllLive()
			return m, tea.Quit
		case "d":
			m.lastAction = "detach"
			// Detach semantics (leave sessions agentless) live in the session
			// manager; the model just signals quit.
			return m, tea.Quit
		case "c":
			m.lastAction = "cancel"
			m.st = stateList
			return m, nil
		}
	case msg.Type == tea.KeyEsc:
		m.lastAction = "cancel"
		m.st = stateList
		return m, nil
	case msg.Type == tea.KeyEnter:
		// Default action is Kill all.
		m.lastAction = "killall"
		m.clearAllLive()
		return m, tea.Quit
	}
	return m, nil
}

// clearAllLive clears the green-dot flag on every entry (kill all sessions).
func (m Model) clearAllLive() {
	for _, e := range m.hostList.All() {
		if e.Live {
			m.hostList.MarkDead(e.Alias, e.Source)
		}
	}
}