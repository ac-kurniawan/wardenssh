package tviewui_test

import (
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/ac-kurniawan/wardenssh/internal/tviewui"
)

// startRealApp boots a real tview Application on a tcell simulation screen,
// so key events travel through the full routing path (screen -> app capture ->
// handleGlobalKeys -> widget). Returns the app and the screen for injection.
func startRealApp(t *testing.T) (*tviewui.App, tcell.SimulationScreen) {
	t.Helper()
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatalf("init simulation screen: %v", err)
	}
	screen.SetSize(120, 30)

	app := tviewui.New(sampleHostList(), tviewui.Deps{}, nil)
	app.SetScreenForTest(screen)

	runDone := make(chan error, 1)
	go func() { runDone <- app.Run() }()
	t.Cleanup(func() {
		app.StopForTest()
		select {
		case <-runDone:
		case <-time.After(3 * time.Second):
			t.Error("app did not stop cleanly")
		}
		screen.Fini()
	})

	// Let the event loop spin up.
	time.Sleep(100 * time.Millisecond)
	return app, screen
}

// waitForPane polls until app.FocusedPane() equals want (or fails).
func waitForPane(t *testing.T, app *tviewui.App, want string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if app.FocusedPane() == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("FocusedPane = %q, want %q (key routing failed)", app.FocusedPane(), want)
}

// TestRealKeyRoutingPaneSwitching: Tab and Ctrl+\ / Ctrl+B move focus host<->terminal
// through the REAL tview event path (screen -> app input capture), while Ctrl+S
// opens the scope switcher. This is the regression test for "Tab doesn't work" /
// "Ctrl+\ doesn't work" / "Ctrl+B can't move right-left".
func TestRealKeyRoutingPaneSwitching(t *testing.T) {
	app, screen := startRealApp(t)

	// Precondition: no session, host pane focused.
	if app.FocusedPane() != "host" {
		t.Fatalf("initial FocusedPane = %q, want host", app.FocusedPane())
	}

	// Ctrl+S with no session opens the scope modal.
	screen.InjectKey(tcell.KeyCtrlS, 0, tcell.ModNone)
	waitForBool(t, func() bool { return app.InScopeModal() })
	app.CancelScopeModal()
	if app.InScopeModal() {
		t.Fatal("expected scope modal dismissed")
	}

	// Register a session and show the terminal pane (real layout tree).
	key := tviewui.SessionKey("prod-db-01", "file")
	app.TerminalPane().SetSessionForTest(key, "prod-db-01", "file")
	app.ShowTerminalPaneForTest()
	waitForPane(t, app, "terminal")

	// Ctrl+\ returns to the host list.
	screen.InjectKey(tcell.KeyCtrlBackslash, 0, tcell.ModNone)
	waitForPane(t, app, "host")

	// Tab from the host list focuses the terminal.
	screen.InjectKey(tcell.KeyTab, 0, tcell.ModNone)
	waitForPane(t, app, "terminal")

	// Ctrl+B from the terminal returns to the host list.
	screen.InjectKey(tcell.KeyCtrlB, 0, tcell.ModNone)
	waitForPane(t, app, "host")

	// Ctrl+B from the host list returns to the terminal (session running).
	screen.InjectKey(tcell.KeyCtrlB, 0, tcell.ModNone)
	waitForPane(t, app, "terminal")

	// Ctrl+S opens scope switcher from terminal; cancelling returns to terminal.
	screen.InjectKey(tcell.KeyCtrlS, 0, tcell.ModNone)
	waitForBool(t, func() bool { return app.InScopeModal() })
	app.CancelScopeModal()
	waitForPane(t, app, "terminal")

	// Tab in the terminal is forwarded (no pane change).
	screen.InjectKey(tcell.KeyTab, 0, tcell.ModNone)
	time.Sleep(100 * time.Millisecond)
	if app.FocusedPane() != "terminal" {
		t.Errorf("Tab in terminal must be forwarded, FocusedPane = %q", app.FocusedPane())
	}

	// Ctrl+Shift+Q immediately hard-closes the active session and returns to host list.
	screen.InjectKey(tcell.KeyCtrlQ, 'Q', tcell.ModCtrl|tcell.ModShift)
	waitForPane(t, app, "host")
	if app.TerminalPane().IsRunning() {
		t.Error("expected session terminated after Ctrl+Shift+Q")
	}
}

// waitForBool polls a condition (shared with key routing tests).
func waitForBool(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition never became true")
}