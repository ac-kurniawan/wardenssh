package tviewui_test

import (
	"testing"

	"github.com/ac-kurniawan/wardenssh/internal/hosts"
	"github.com/ac-kurniawan/wardenssh/internal/tviewui"
)

func prodEntry() hosts.Entry {
	return hosts.Entry{Alias: "prod-db-01", HostName: "10.0.0.5", Source: "file"}
}

func webEntry() hosts.Entry {
	return hosts.Entry{Alias: "web-02", HostName: "web.internal", Source: "file"}
}

// TestHandleConnectSameHostShowsDisconnectModal: pressing Enter on a host that
// already has a live session must open the disconnect confirmation modal
// (NOT silently kill the green dot, NOT spawn a duplicate).
func TestHandleConnectSameHostShowsDisconnectModal(t *testing.T) {
	hl := sampleHostList()
	app := tviewui.New(hl, tviewui.Deps{}, nil)
	key := tviewui.SessionKey(prodEntry().Alias, prodEntry().Source)
	app.TerminalPane().SetSessionForTest(key, prodEntry().Alias, prodEntry().Source)
	hl.MarkLive(prodEntry().Alias, prodEntry().Source)

	app.HandleConnectForTest(prodEntry())

	if !app.InDisconnect() {
		t.Fatal("expected disconnect confirmation modal for a live host")
	}
	// The session and green dot must still be intact (modal is pending).
	if !app.TerminalPane().HasSession(key) {
		t.Error("session must survive while the disconnect modal is open")
	}
	if !isLive(hl, prodEntry().Alias, prodEntry().Source) {
		t.Error("green dot must survive while the disconnect modal is open")
	}
}

// TestHandleConnectSameHostCancelKeepsSession: cancelling the disconnect modal
// leaves the session running and the host live.
func TestHandleConnectSameHostCancelKeepsSession(t *testing.T) {
	hl := sampleHostList()
	app := tviewui.New(hl, tviewui.Deps{}, nil)
	key := tviewui.SessionKey(prodEntry().Alias, prodEntry().Source)
	app.TerminalPane().SetSessionForTest(key, prodEntry().Alias, prodEntry().Source)
	hl.MarkLive(prodEntry().Alias, prodEntry().Source)

	app.HandleConnectForTest(prodEntry())
	if !app.InDisconnect() {
		t.Fatal("precondition: disconnect modal should be open")
	}
	app.CancelDisconnect()

	if app.InDisconnect() {
		t.Fatal("expected disconnect modal dismissed after cancel")
	}
	if !app.TerminalPane().HasSession(key) {
		t.Error("expected session to keep running after cancel")
	}
	if !isLive(hl, prodEntry().Alias, prodEntry().Source) {
		t.Error("expected green dot to stay after cancel")
	}
}

// TestHandleConnectSameHostConfirmDisconnects: confirming the disconnect modal
// closes only that session and clears its green dot.
func TestHandleConnectSameHostConfirmDisconnects(t *testing.T) {
	hl := sampleHostList()
	app := tviewui.New(hl, tviewui.Deps{}, nil)
	key := tviewui.SessionKey(prodEntry().Alias, prodEntry().Source)
	app.TerminalPane().SetSessionForTest(key, prodEntry().Alias, prodEntry().Source)
	hl.MarkLive(prodEntry().Alias, prodEntry().Source)

	app.HandleConnectForTest(prodEntry())
	if !app.InDisconnect() {
		t.Fatal("precondition: disconnect modal should be open")
	}
	app.ConfirmDisconnect()

	if app.InDisconnect() {
		t.Fatal("expected disconnect modal dismissed after confirm")
	}
	if app.TerminalPane().HasSession(key) {
		t.Error("expected session closed after confirm")
	}
	if isLive(hl, prodEntry().Alias, prodEntry().Source) {
		t.Error("expected green dot cleared after disconnect")
	}
}

// TestHandleConnectBackgroundSessionSwitchesToIt: Enter on a host whose
// session is running in the BACKGROUND activates it (yield-and-switch), it
// does not spawn a duplicate and does not open a disconnect modal.
func TestHandleConnectBackgroundSessionSwitchesToIt(t *testing.T) {
	hl := sampleHostList()
	app := tviewui.New(hl, tviewui.Deps{}, nil)
	prodKey := tviewui.SessionKey(prodEntry().Alias, prodEntry().Source)
	webKey := tviewui.SessionKey(webEntry().Alias, webEntry().Source)
	app.TerminalPane().SetSessionForTest(prodKey, prodEntry().Alias, prodEntry().Source)
	app.TerminalPane().SetSessionForTest(webKey, webEntry().Alias, webEntry().Source)
	hl.MarkLive(prodEntry().Alias, prodEntry().Source)
	hl.MarkLive(webEntry().Alias, webEntry().Source)

	// web-02 is the active session; prod-db-01 runs in the background.
	app.TerminalPane().Activate(webKey)

	app.HandleConnectForTest(prodEntry())

	if app.InDisconnect() {
		t.Fatal("selecting a background session must NOT open the disconnect modal")
	}
	if activeAlias, _, ok := app.TerminalPane().ActiveEntry(); !ok || activeAlias != prodEntry().Alias {
		t.Errorf("ActiveEntry = %q/%v, want %q/true", activeAlias, ok, prodEntry().Alias)
	}
	if n := app.TerminalPane().SessionCount(); n != 2 {
		t.Errorf("SessionCount = %d, want 2 (no duplicate spawn)", n)
	}
}

// TestAppKillAllQuitClosesTerminalSessions: Kill-all quits the app and closes
// every terminal session (green dots cleared).
func TestAppKillAllQuitClosesTerminalSessions(t *testing.T) {
	hl := sampleHostList()
	app := tviewui.New(hl, tviewui.Deps{}, nil)
	key := tviewui.SessionKey(prodEntry().Alias, prodEntry().Source)
	app.TerminalPane().SetSessionForTest(key, prodEntry().Alias, prodEntry().Source)
	hl.MarkLive(prodEntry().Alias, prodEntry().Source)

	app.RequestQuit()
	if !app.InQuitModal() {
		t.Fatal("precondition: quit modal should be open")
	}
	app.KillAllQuit()

	if app.TerminalPane().SessionCount() != 0 {
		t.Error("expected all terminal sessions closed after kill-all")
	}
	if liveCountOf(app) != 0 {
		t.Error("expected all green dots cleared after kill-all")
	}
}

func isLive(l *hosts.List, alias, source string) bool {
	for _, e := range l.All() {
		if e.Alias == alias && e.Source == source && e.Live {
			return true
		}
	}
	return false
}
