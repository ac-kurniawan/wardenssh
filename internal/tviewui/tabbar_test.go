package tviewui_test

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/ac-kurniawan/wardenssh/internal/tviewui"
)

// TestTabBarClickSwitchesSession: clicking a background tab (MouseLeftDown on
// its TextView) must fire onSelect with that tab's key. The current code
// listens for MouseLeftClick, which never fires because MouseLeftDown steals
// focus and consumes the event.
func TestTabBarClickSwitchesSession(t *testing.T) {
	bar := tviewui.NewSessionTabBar()

	selected := ""
	bar.SetOnSelect(func(key string) { selected = key })

	keys := []string{"host-a\x00file", "host-b\x00file", "host-c\x00file"}
	aliases := map[string]string{
		"host-a\x00file": "host-a",
		"host-b\x00file": "host-b",
		"host-c\x00file": "host-c",
	}
	hosts := map[string]string{
		"host-a\x00file": "10.0.0.1",
		"host-b\x00file": "10.0.0.2",
		"host-c\x00file": "10.0.0.3",
	}
	bar.Update(keys, "host-a\x00file", aliases, hosts)

	if bar.ActiveKey() != "host-a\x00file" {
		t.Fatalf("precondition: active = %q, want host-a", bar.ActiveKey())
	}

	// Get the second tab's TextView and fire MouseLeftDown on it.
	flex := bar.Primitive().(*tview.Flex)
	if flex.GetItemCount() < 2 {
		t.Fatalf("expected at least 2 tab items, got %d", flex.GetItemCount())
	}
	tab := flex.GetItem(1).(*tview.TextView)
	handler := tab.MouseHandler()
	if handler == nil {
		t.Fatal("tab has no mouse handler")
	}

	// Simulate click at position (1,0) inside the tab.
	handler(tview.MouseLeftDown, tcell.NewEventMouse(1, 0, tcell.Button1, tcell.ModNone), func(p tview.Primitive) {})

	if selected != "host-b\x00file" {
		t.Errorf("click on tab[1]: onSelect got %q, want %q", selected, "host-b\x00file")
	}
}

// TestCtrlPgDnCyclesToNextSession: Ctrl+PgDn in the terminal pane must switch
// to the next session tab (browser-style tab cycling).
func TestCtrlPgDnCyclesToNextSession(t *testing.T) {
	hl := sampleHostList()
	app := tviewui.New(hl, tviewui.Deps{}, nil)

	// Register 3 sessions.
	app.TerminalPane().SetSessionForTest("host-a\x00file", "host-a", "file")
	app.TerminalPane().SetSessionForTest("host-b\x00file", "host-b", "file")
	app.TerminalPane().SetSessionForTest("host-c\x00file", "host-c", "file")
	app.FocusTerminal()

	if app.FocusedPane() != "terminal" {
		t.Fatalf("precondition: FocusedPane = %q, want terminal", app.FocusedPane())
	}

	active := app.TabBar().ActiveKey()
	if active == "" {
		t.Fatal("precondition: no active tab")
	}

	// Ctrl+PgDn should cycle to next.
	ev := app.HandleGlobalKey(tcell.NewEventKey(tcell.KeyPgDn, 0, tcell.ModCtrl))
	if ev != nil {
		t.Error("Ctrl+PgDn should be consumed (returned non-nil event)")
	}

	newActive := app.TabBar().ActiveKey()
	if newActive == active {
		t.Errorf("Ctrl+PgDn did not switch session: active still %q", active)
	}
}

// TestCtrlPgUpCyclesToPrevSession: Ctrl+PgUp in the terminal pane must switch
// to the previous session tab.
func TestCtrlPgUpCyclesToPrevSession(t *testing.T) {
	hl := sampleHostList()
	app := tviewui.New(hl, tviewui.Deps{}, nil)

	// Register 3 sessions.
	app.TerminalPane().SetSessionForTest("host-a\x00file", "host-a", "file")
	app.TerminalPane().SetSessionForTest("host-b\x00file", "host-b", "file")
	app.TerminalPane().SetSessionForTest("host-c\x00file", "host-c", "file")
	app.FocusTerminal()

	if app.FocusedPane() != "terminal" {
		t.Fatalf("precondition: FocusedPane = %q, want terminal", app.FocusedPane())
	}

	active := app.TabBar().ActiveKey()
	if active == "" {
		t.Fatal("precondition: no active tab")
	}

	// Ctrl+PgUp should cycle to previous.
	ev := app.HandleGlobalKey(tcell.NewEventKey(tcell.KeyPgUp, 0, tcell.ModCtrl))
	if ev != nil {
		t.Error("Ctrl+PgUp should be consumed (returned non-nil event)")
	}

	newActive := app.TabBar().ActiveKey()
	if newActive == active {
		t.Errorf("Ctrl+PgUp did not switch session: active still %q", active)
	}
}

// TestSessionCycleHotkeysWrapAround: cycling past the last/first tab wraps.
func TestSessionCycleHotkeysWrapAround(t *testing.T) {
	hl := sampleHostList()
	app := tviewui.New(hl, tviewui.Deps{}, nil)

	app.TerminalPane().SetSessionForTest("host-a\x00file", "host-a", "file")
	app.TerminalPane().SetSessionForTest("host-b\x00file", "host-b", "file")
	app.FocusTerminal()

	// Cycle forward through all and confirm we wrap back to start.
	initial := app.TabBar().ActiveKey()
	app.HandleGlobalKey(tcell.NewEventKey(tcell.KeyPgDn, 0, tcell.ModCtrl))
	mid := app.TabBar().ActiveKey()
	app.HandleGlobalKey(tcell.NewEventKey(tcell.KeyPgDn, 0, tcell.ModCtrl))
	final := app.TabBar().ActiveKey()

	if mid == initial {
		t.Error("first Ctrl+PgDn did not switch")
	}
	if final != initial {
		t.Errorf("after cycling through all tabs: got %q, want wrap to %q", final, initial)
	}
}
