package tviewui_test

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/ac-kurniawan/wardenssh/internal/tviewui"
)

// escEvent builds an Escape key event.
func escEvent() *tcell.EventKey {
	return tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone)
}

// TestAppEscInTerminalForwardsToTerminal: when the terminal pane is focused,
// ESC must be passed through to the terminal (so vim/less receive it), NOT
// move focus back to the host list. It must also NOT open the quit modal and
// NOT stop the session.
func TestAppEscInTerminalForwardsToTerminal(t *testing.T) {
	hl := sampleHostList()
	app := tviewui.New(hl, tviewui.Deps{}, nil)

	// Simulate: a session is running and the terminal pane has focus.
	app.TerminalPane().SetRunningForTest(true)
	app.FocusTerminal()
	if app.FocusedPane() != "terminal" {
		t.Fatalf("precondition: FocusedPane = %q, want terminal", app.FocusedPane())
	}

	ev := app.HandleGlobalKey(escEvent())
	if ev == nil {
		t.Fatal("ESC in terminal: event = nil, want forwarded to the terminal")
	}
	if app.FocusedPane() != "terminal" {
		t.Errorf("FocusedPane = %q, want terminal after ESC in terminal", app.FocusedPane())
	}
	if app.InQuitModal() {
		t.Error("ESC in terminal must NOT open the quit modal")
	}
	if !app.TerminalPane().IsRunning() {
		t.Error("session must stay running after ESC in terminal")
	}
}

// TestAppTabFocusesTerminalAndCtrlBackslashReturns: per the revamp keymap,
// Tab (hosts pane) moves focus into the terminal PTY and Ctrl+\ releases focus
// back to the sidebar.
func TestAppTabFocusesTerminalAndCtrlBackslashReturns(t *testing.T) {
	hl := sampleHostList()
	app := tviewui.New(hl, tviewui.Deps{}, nil)

	// No session: Tab stays on host list (nothing to focus).
	ev := app.HandleGlobalKey(tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone))
	if app.FocusedPane() != "host" {
		t.Errorf("Tab with no session: FocusedPane = %q, want host", app.FocusedPane())
	}

	// Session running: Tab moves into the terminal, Ctrl+\ returns.
	app.TerminalPane().SetRunningForTest(true)
	ev = app.HandleGlobalKey(tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone))
	if app.FocusedPane() != "terminal" {
		t.Errorf("Tab: FocusedPane = %q, want terminal", app.FocusedPane())
	}
	_ = ev
	ev = app.HandleGlobalKey(tcell.NewEventKey(tcell.KeyCtrlBackslash, 0, tcell.ModNone))
	if app.FocusedPane() != "host" {
		t.Errorf("Ctrl+\\: FocusedPane = %q, want host", app.FocusedPane())
	}
	_ = ev
}

// TestAppCtrlBInHostOpensScopeModal: Ctrl+B with NO session opens the scope
// switcher overlay (there is no pane to toggle to).
func TestAppCtrlBInHostOpensScopeModal(t *testing.T) {
	hl := sampleHostList()
	app := tviewui.New(hl, tviewui.Deps{}, nil)
	if app.InScopeModal() {
		t.Fatal("precondition: not in scope modal")
	}
	app.HandleGlobalKey(tcell.NewEventKey(tcell.KeyCtrlB, 0, tcell.ModNone))
	if !app.InScopeModal() {
		t.Fatal("Ctrl+B must open the scope modal")
	}
	app.CancelScopeModal()
	if app.InScopeModal() {
		t.Fatal("expected scope modal dismissed")
	}
}

// TestAppCtrlBAlwaysOpensScopeModal: Ctrl+B opens the scope switcher regardless
// of whether a session is running — it is the scope hotkey, NOT a pane toggle
// (regression: Ctrl+B was overloaded for pane-switching, orphaning the scope
// switcher while a session ran).
func TestAppCtrlBAlwaysOpensScopeModal(t *testing.T) {
	hl := sampleHostList()
	app := tviewui.New(hl, tviewui.Deps{}, nil)
	app.TerminalPane().SetRunningForTest(true)

	app.HandleGlobalKey(tcell.NewEventKey(tcell.KeyCtrlB, 0, tcell.ModNone))
	if !app.InScopeModal() {
		t.Fatal("Ctrl+B must open the scope modal even with a session running")
	}
	app.CancelScopeModal()
	if app.InScopeModal() {
		t.Fatal("expected scope modal dismissed")
	}
}

// TestAppEscInTerminalForwardsOtherKeys: keys other than Ctrl+B/Ctrl+C are
// passed through to the terminal (never intercepted for quit). ESC is one of
// them — vim/less need it.
func TestAppEscInTerminalForwardsOtherKeys(t *testing.T) {
	hl := sampleHostList()
	app := tviewui.New(hl, tviewui.Deps{}, nil)
	app.TerminalPane().SetRunningForTest(true)
	app.FocusTerminal()

	for _, tc := range []struct {
		name  string
		event *tcell.EventKey
	}{
		{"q", tcell.NewEventKey(tcell.KeyRune, 'q', tcell.ModNone)},
		{"enter", tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone)},
		{"up", tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone)},
		{"x", tcell.NewEventKey(tcell.KeyRune, 'x', tcell.ModNone)},
		{"esc", escEvent()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ev := app.HandleGlobalKey(tc.event)
			if ev == nil {
				t.Fatalf("%s in terminal: event = nil, want forwarded", tc.name)
			}
			if app.InQuitModal() {
				t.Fatal("key in terminal must not open quit modal")
			}
			if app.FocusedPane() != "terminal" {
				t.Fatalf("FocusedPane = %q, want terminal (key forwarded)", app.FocusedPane())
			}
		})
	}
}

// TestAppEscWithFilterTextClearsFilter: ESC in the host list with a non-empty
// filter clears the filter instead of opening the quit modal.
func TestAppEscWithFilterTextClearsFilter(t *testing.T) {
	hl := sampleHostList()
	app := tviewui.New(hl, tviewui.Deps{}, nil)
	app.HostPane().SetFilter("prod")
	if app.HostPane().FilterText() != "prod" {
		t.Fatalf("precondition: FilterText = %q, want prod", app.HostPane().FilterText())
	}
	ev := app.HandleGlobalKey(escEvent())
	if ev != nil {
		t.Fatalf("ESC with filter text: event = %v, want nil (consumed)", ev)
	}
	if app.InQuitModal() {
		t.Fatal("ESC with filter text must not open quit modal")
	}
	if app.HostPane().FilterText() != "" {
		t.Errorf("FilterText = %q, want empty after ESC", app.HostPane().FilterText())
	}
}

// TestQuitModalEscCancels: pressing ESC in the quit modal cancels it.
func TestQuitModalEscCancels(t *testing.T) {
	m := tviewui.NewQuitModal()
	cancelled := false
	m.SetOnCancel(func() { cancelled = true })

	var focusFunc func(p tview.Primitive)
	focusFunc = func(p tview.Primitive) {}
	handler := m.Primitive().InputHandler()
	if handler != nil {
		handler(escEvent(), focusFunc)
	}
	if !cancelled {
		t.Error("expected ESC in quit modal to cancel")
	}
}

// TestQuitModalKeybindings: k/d/c keys trigger kill-all/detach/cancel.
func TestQuitModalKeybindings(t *testing.T) {
	t.Run("k kills all", func(t *testing.T) {
		m := tviewui.NewQuitModal()
		var got string
		m.SetOnKillAll(func() { got = "killall" })
		m.TriggerKey(tcell.NewEventKey(tcell.KeyRune, 'k', tcell.ModNone))
		if got != "killall" {
			t.Errorf("k: got %q, want killall", got)
		}
	})
	t.Run("d detaches", func(t *testing.T) {
		m := tviewui.NewQuitModal()
		var got string
		m.SetOnDetach(func() { got = "detach" })
		m.TriggerKey(tcell.NewEventKey(tcell.KeyRune, 'd', tcell.ModNone))
		if got != "detach" {
			t.Errorf("d: got %q, want detach", got)
		}
	})
	t.Run("c cancels", func(t *testing.T) {
		m := tviewui.NewQuitModal()
		var got string
		m.SetOnCancel(func() { got = "cancel" })
		m.TriggerKey(tcell.NewEventKey(tcell.KeyRune, 'c', tcell.ModNone))
		if got != "cancel" {
			t.Errorf("c: got %q, want cancel", got)
		}
	})
}
