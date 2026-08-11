package tui_test

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ac-kurniawan/wardenssh/internal/hosts"
	"github.com/ac-kurniawan/wardenssh/internal/tui"
)

// runeKey builds a KeyRunes KeyMsg for the given rune(s).
func runeKey(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func sampleList() *hosts.List {
	return hosts.NewList([]hosts.Entry{
		{Alias: "prod-db-01", HostName: "10.0.0.5", Source: "file"},
		{Alias: "web-02", HostName: "web.internal", Source: "file"},
		{Alias: "ci-box", HostName: "10.1.0.10", Source: "vw:personal"},
	})
}

// update runs Update and returns the typed model, executing the model-test
// seam for the returned command.
func updateOne(m tui.Model, msg tea.Msg) (tui.Model, tea.Msg) {
	mm, cmd := m.Update(msg)
	var result tea.Msg
	if cmd != nil {
		result = cmd()
	}
	return mm.(tui.Model), result
}

// TestQuitNoLiveSessionsQuits: with no live sessions, 'q' returns tea.Quit
// immediately (no modal needed per Q31/C).
func TestQuitNoLiveSessionsQuits(t *testing.T) {
	m := tui.New(sampleList())
	mm, msg := updateOne(m, runeKey("q"))
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Errorf("q with no live sessions: cmd = %T, want tea.QuitMsg", msg)
	}
	if mm.InQuitModal() {
		t.Error("should not enter quit modal when no live sessions")
	}
}

// TestQuitModalOnLiveSessions: when a session is live, 'q' opens the
// confirmation modal instead of quitting (Q31/C: blocked-quit until choice).
func TestQuitModalOnLiveSessions(t *testing.T) {
	h := sampleList()
	h.MarkLive("prod-db-01", "file")
	m := tui.New(h)
	mm, msg := updateOne(m, runeKey("q"))
	if mm.InQuitModal() {
		// good
	} else {
		t.Fatal("q with live session: should enter quit modal")
	}
	if _, ok := msg.(tea.QuitMsg); ok {
		t.Error("should not quit yet; modal must be answered first")
	}
}

// TestQuitModalKillAllDefaultsAndQuits: the default modal action ('k' or Enter)
// is Kill all; it clears live flags and quits.
func TestQuitModalKillAll(t *testing.T) {
	h := sampleList()
	h.MarkLive("prod-db-01", "file")
	m := tui.New(h)
	m, _ = updateOne(m, runeKey("q"))
	mm, msg := updateOne(m, runeKey("k"))
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Errorf("kill all: cmd = %T, want tea.QuitMsg", msg)
	}
	if mm.LastAction() != "killall" {
		t.Errorf("LastAction = %q, want killall", mm.LastAction())
	}
	if len(mm.List().Visible()) != 0 && anyLive(mm.List()) {
		t.Error("kill all should have cleared live sessions")
	}
}

// TestQuitModalCancelReturnsToList: 'c' in the modal returns to the list and
// does not quit.
func TestQuitModalCancelReturnsToList(t *testing.T) {
	h := sampleList()
	h.MarkLive("prod-db-01", "file")
	m := tui.New(h)
	m, _ = updateOne(m, runeKey("q"))
	mm, msg := updateOne(m, runeKey("c"))
	if mm.InQuitModal() {
		t.Error("cancel should exit the modal")
	}
	if msg != nil {
		t.Errorf("cancel: cmd = %T, want nil", msg)
	}
	if mm.LastAction() != "cancel" {
		t.Errorf("LastAction = %q, want cancel", mm.LastAction())
	}
}

// TestTabCyclesScope: Tab advances the host-list scope (Q29/B cycle).
func TestTabCyclesScope(t *testing.T) {
	m := tui.New(sampleList())
	before := m.List().Scope()
	m, _ = updateOne(m, tea.KeyMsg{Type: tea.KeyTab})
	after := m.List().Scope()
	if before == after {
		t.Errorf("Tab did not advance scope: before=%q after=%q", before, after)
	}
}

// TestFilterInputAppendsAndNarrows: typing runes appends to the filter and
// narrows the visible list.
func TestFilterInputAppendsAndNarrows(t *testing.T) {
	m := tui.New(sampleList())
	for _, r := range []string{"p", "r", "o", "d"} {
		m, _ = updateOne(m, runeKey(r))
	}
	if got := m.Filter(); got != "prod" {
		t.Errorf("Filter = %q, want prod", got)
	}
	vis := m.List().Visible()
	// prod matches both prod-db-01 and (via subsequence) the vault? no — 'vw' entries
	// have aliases ci-box; none contain "prod". So expect just prod-db-01.
	if len(vis) != 1 || vis[0].Alias != "prod-db-01" {
		t.Errorf("visible = %+v, want [prod-db-01]", vis)
	}
}

// TestBackspaceTrimsFilter: Backspace removes the last filter character.
func TestBackspaceTrimsFilter(t *testing.T) {
	m := tui.New(sampleList())
	for _, r := range []string{"p", "r", "o", "d"} {
		m, _ = updateOne(m, runeKey(r))
	}
	m, _ = updateOne(m, tea.KeyMsg{Type: tea.KeyBackspace})
	if got := m.Filter(); got != "pro" {
		t.Errorf("Filter after backspace = %q, want pro", got)
	}
}

// TestEnterEmitsConnectIntent: pressing Enter on the selected entry emits a
// ConnectMsg carrying that entry (the real connect is the session manager's
// job, tested separately; here we verify the model's intent signal).
func TestEnterEmitsConnectIntent(t *testing.T) {
	m := tui.New(sampleList())
	m, msg := updateOne(m, tea.KeyMsg{Type: tea.KeyEnter})
	cm, ok := msg.(tui.ConnectMsg)
	if !ok {
		t.Fatalf("Enter: cmd = %T, want tui.ConnectMsg", msg)
	}
	if cm.Entry.Alias != "prod-db-01" {
		t.Errorf("ConnectMsg.Entry.Alias = %q, want prod-db-01", cm.Entry.Alias)
	}
}

// TestCtrlCActsLikeQuit: Ctrl+C (SIGINT) is intercepted like 'q' — quit when
// no live sessions, modal when there are (Q31/C: SIGINT must not instant-kill).
func TestCtrlCActsLikeQuit(t *testing.T) {
	t.Run("no live -> quit", func(t *testing.T) {
		m := tui.New(sampleList())
		_, msg := updateOne(m, tea.KeyMsg{Type: tea.KeyCtrlC})
		if _, ok := msg.(tea.QuitMsg); !ok {
			t.Errorf("Ctrl+C no live: cmd = %T, want tea.QuitMsg", msg)
		}
	})
	t.Run("live -> modal", func(t *testing.T) {
		h := sampleList()
		h.MarkLive("ci-box", "vw:personal")
		m := tui.New(h)
		mm, msg := updateOne(m, tea.KeyMsg{Type: tea.KeyCtrlC})
		if !mm.InQuitModal() {
			t.Error("Ctrl+C with live: should enter modal, not instant-kill")
		}
		if msg != nil {
			t.Errorf("Ctrl+C live: cmd = %T, want nil (modal blocks quit)", msg)
		}
	})
}

// TestSessionExitedMsgClearsLiveFlag: receiving SessionExitedMsg marks the host dead.
func TestSessionExitedMsgClearsLiveFlag(t *testing.T) {
	h := sampleList()
	h.MarkLive("prod-db-01", "file")
	m := tui.New(h)

	mm, _ := m.Update(tui.SessionExitedMsg{
		Alias:     "prod-db-01",
		Source:    "file",
		SessionID: "s123",
	})

	mModel := mm.(tui.Model)
	for _, e := range mModel.List().All() {
		if e.Alias == "prod-db-01" && e.Live {
			t.Errorf("expected prod-db-01 to be dead after SessionExitedMsg")
		}
	}
}

func anyLive(l *hosts.List) bool {
	for _, e := range l.Visible() {
		if e.Live {
			return true
		}
	}
	return false
}