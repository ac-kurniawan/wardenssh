package tviewui

import (
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/ac-kurniawan/wardenssh/internal/hosts"
)

func copyTestHostList() *hosts.List {
	return hosts.NewList([]hosts.Entry{
		{Alias: "prod-db-01", HostName: "10.0.0.5", Source: "file"},
	})
}

func copyTestKey() string { return SessionKey("host-a", "file") }

// TestTerminalPaneCopyActiveSelection: copying the active session's selection
// writes the selected text to the clipboard and clears the highlight.
func TestTerminalPaneCopyActiveSelection(t *testing.T) {
	pane := NewTerminalPane(nil)
	defer pane.Close()
	pane.SetSessionForTest(copyTestKey(), "host-a", "file")

	view, _ := fedView(t)
	if !view.StartSelection(1, 1) || !view.UpdateSelection(5, 2) {
		t.Fatal("precondition: failed to create a selection")
	}
	pane.SetSessionViewForTest(copyTestKey(), view)

	var copied string
	orig := copySelection
	copySelection = func(text string) error { copied = text; return nil }
	defer func() { copySelection = orig }()

	if !pane.CopyActiveSelection() {
		t.Fatal("expected an active selection to be copied")
	}
	if copied != "abcdefghij\nklmno" {
		t.Errorf("copied = %q, want %q", copied, "abcdefghij\nklmno")
	}
	if view.HasSelection() {
		t.Error("expected the selection to be cleared after copy")
	}
}

// TestTerminalPaneCopyActiveSelectionNone: CopyActiveSelection reports false
// and leaves the view untouched when no selection exists.
func TestTerminalPaneCopyActiveSelectionNone(t *testing.T) {
	pane := NewTerminalPane(nil)
	defer pane.Close()
	pane.SetSessionForTest(copyTestKey(), "host-a", "file")

	view, _ := fedView(t)
	pane.SetSessionViewForTest(copyTestKey(), view)

	if pane.CopyActiveSelection() {
		t.Error("expected false when no selection exists")
	}
}

// TestAppCtrlCCopiesSelectionWhenPresent: Ctrl+C in the terminal pane with an
// active selection copies it and is consumed (not forwarded to the remote as
// SIGINT).
func TestAppCtrlCCopiesSelectionWhenPresent(t *testing.T) {
	app := New(copyTestHostList(), Deps{}, nil)
	app.TerminalPane().SetSessionForTest(copyTestKey(), "host-a", "file")

	view, _ := fedView(t)
	if !view.StartSelection(1, 1) || !view.UpdateSelection(5, 2) {
		t.Fatal("precondition: failed to create a selection")
	}
	app.TerminalPane().SetSessionViewForTest(copyTestKey(), view)

	app.FocusTerminal()
	if app.FocusedPane() != "terminal" {
		t.Fatalf("precondition: FocusedPane = %q, want terminal", app.FocusedPane())
	}

	var copied string
	orig := copySelection
	copySelection = func(text string) error { copied = text; return nil }
	defer func() { copySelection = orig }()

	ev := app.HandleGlobalKey(tcell.NewEventKey(tcell.KeyCtrlC, 0, tcell.ModNone))
	if ev != nil {
		t.Fatalf("Ctrl+C with selection: event = %v, want nil (consumed for copy)", ev)
	}
	if copied != "abcdefghij\nklmno" {
		t.Errorf("copied = %q, want %q", copied, "abcdefghij\nklmno")
	}
	if view.HasSelection() {
		t.Error("expected the selection to be cleared after copy")
	}
}

// TestAppCtrlCForwardsSIGINTWithoutSelection: Ctrl+C with no selection is still
// forwarded to the terminal as SIGINT (existing behavior preserved).
func TestAppCtrlCForwardsSIGINTWithoutSelection(t *testing.T) {
	app := New(copyTestHostList(), Deps{}, nil)
	app.TerminalPane().SetRunningForTest(true)
	app.FocusTerminal()
	if app.FocusedPane() != "terminal" {
		t.Fatalf("precondition: FocusedPane = %q, want terminal", app.FocusedPane())
	}

	ev := app.HandleGlobalKey(tcell.NewEventKey(tcell.KeyCtrlC, 0, tcell.ModNone))
	if ev == nil {
		t.Fatal("Ctrl+C without selection must be forwarded as SIGINT")
	}
}
