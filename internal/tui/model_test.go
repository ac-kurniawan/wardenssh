package tui_test

import (
	"fmt"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ac-kurniawan/wardenssh/internal/config"
	"github.com/ac-kurniawan/wardenssh/internal/hosts"
	"github.com/ac-kurniawan/wardenssh/internal/tui"
	"github.com/ac-kurniawan/wardenssh/internal/vaultadapter"
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

// --- Setup modal tests ---

func sampleVaults() []config.Vault {
	return []config.Vault{
		{Name: "vw", Server: "https://vw.example.com", Email: "user@example.com"},
	}
}

// TestSetupModalInitialState: NewWithSetup produces a model in setup state
// with the textinput focused and the first vault's prompt shown.
func TestSetupModalInitialState(t *testing.T) {
	m := tui.NewWithSetup(sampleList(), tui.Deps{}, sampleVaults())

	if !m.InSetup() {
		t.Fatal("expected model to be in setup state")
	}
	if m.SetupPrompt() == "" {
		t.Fatal("expected non-empty setup prompt")
	}
	// The prompt should mention the vault name and email.
	prompt := m.SetupPrompt()
	if !contains(prompt, "vw") {
		t.Errorf("prompt should contain vault name 'vw', got: %s", prompt)
	}
	if !contains(prompt, "user@example.com") {
		t.Errorf("prompt should contain email, got: %s", prompt)
	}
}

// TestSetupModalTypingBuildsPassword: typing runes into the setup modal
// accumulates into the password field (masked).
func TestSetupModalTypingBuildsPassword(t *testing.T) {
	m := tui.NewWithSetup(sampleList(), tui.Deps{}, sampleVaults())

	mm, _ := m.Update(runeKey("p"))
	mm, _ = mm.Update(runeKey("a"))
	mm, _ = mm.Update(runeKey("s"))
	mm, _ = mm.Update(runeKey("s"))

	mModel := mm.(tui.Model)
	if got := mModel.SetupPassword(); got != "pass" {
		t.Errorf("expected password 'pass', got %q", got)
	}
}

// TestSetupModalBackspaceDeletesLastChar: Backspace removes the last char.
func TestSetupModalBackspaceDeletesLastChar(t *testing.T) {
	m := tui.NewWithSetup(sampleList(), tui.Deps{}, sampleVaults())

	mm, _ := m.Update(runeKey("h"))
	mm, _ = mm.Update(runeKey("i"))
	mm, _ = mm.Update(tea.KeyMsg{Type: tea.KeyBackspace})

	mModel := mm.(tui.Model)
	if got := mModel.SetupPassword(); got != "h" {
		t.Errorf("expected 'h' after backspace, got %q", got)
	}
}

// TestSetupModalEscSkipsVault: Esc transitions to list state (graceful
// degradation — file-only hosts, no vault).
func TestSetupModalEscSkipsVault(t *testing.T) {
	m := tui.NewWithSetup(sampleList(), tui.Deps{}, sampleVaults())

	mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	mModel := mm.(tui.Model)

	if mModel.InSetup() {
		t.Fatal("expected model to leave setup state after Esc")
	}
	if mModel.VaultClient() != nil {
		t.Fatal("expected nil vault client after skipping vault")
	}
}

// TestSetupModalEnterEmitsLoginCmd: Enter returns a command (the login cmd).
func TestSetupModalEnterEmitsLoginCmd(t *testing.T) {
	m := tui.NewWithSetup(sampleList(), tui.Deps{}, sampleVaults())

	// Type a password first.
	mm, _ := m.Update(runeKey("secret"))
	// Enter should produce a command.
	_, cmd := mm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected non-nil command on Enter in setup state")
	}
}

// TestSetupModalEscMultiVaultSkipsAll: with multiple vaults, Esc skips the
// current vault and advances to the next. A second Esc skips that too.
func TestSetupModalEscMultiVaultSkipsAll(t *testing.T) {
	vaults := []config.Vault{
		{Name: "vw1", Server: "https://vw1.example.com", Email: "u1@e.com"},
		{Name: "vw2", Server: "https://vw2.example.com", Email: "u2@e.com"},
	}
	m := tui.NewWithSetup(sampleList(), tui.Deps{}, vaults)

	// First Esc skips vw1, should advance to vw2.
	mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	mModel := mm.(tui.Model)
	if !mModel.InSetup() {
		t.Fatal("expected to still be in setup for vw2 after skipping vw1")
	}
	if !contains(mModel.SetupPrompt(), "vw2") {
		t.Errorf("expected prompt for vw2, got: %s", mModel.SetupPrompt())
	}

	// Second Esc skips vw2, transitions to list.
	mm2, _ := mm.Update(tea.KeyMsg{Type: tea.KeyEsc})
	mModel2 := mm2.(tui.Model)
	if mModel2.InSetup() {
		t.Fatal("expected to leave setup after skipping all vaults")
	}
}

// TestSetupModalShowsErrorOnLoginFailure: receiving a VaultErrorMsg sets the
// setup error message and stays in setup state for retry.
func TestSetupModalShowsErrorOnLoginFailure(t *testing.T) {
	m := tui.NewWithSetup(sampleList(), tui.Deps{}, sampleVaults())

	mm, _ := m.Update(tui.VaultErrorMsg{Err: fmt.Errorf("invalid master password")})
	mModel := mm.(tui.Model)

	if !mModel.InSetup() {
		t.Fatal("expected to stay in setup after login error")
	}
	if mModel.SetupError() == "" {
		t.Fatal("expected non-empty setup error after login failure")
	}
	if !contains(mModel.SetupError(), "invalid master password") {
		t.Errorf("expected error to mention 'invalid master password', got: %s", mModel.SetupError())
	}
	// Password should be cleared for retry.
	if mModel.SetupPassword() != "" {
		t.Errorf("expected password cleared after error, got %q", mModel.SetupPassword())
	}
}

// TestSetupModalVaultReadyTransitionsToList: receiving VaultReadyMsg transitions
// to list state with a non-nil vault client.
func TestSetupModalVaultReadyTransitionsToList(t *testing.T) {
	m := tui.NewWithSetup(sampleList(), tui.Deps{}, sampleVaults())

	// Simulate a successful login by sending VaultReadyMsg with a fake source.
	mm, _ := m.Update(tui.VaultReadyMsg{Source: &vaultadapter.Source{}})
	mModel := mm.(tui.Model)

	if mModel.InSetup() {
		t.Fatal("expected to leave setup after VaultReadyMsg")
	}
	if mModel.VaultClient() == nil {
		t.Fatal("expected non-nil vault client after successful login")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && indexOf(s, substr) >= 0))
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}