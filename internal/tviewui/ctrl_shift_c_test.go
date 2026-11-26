package tviewui

import (
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
)

// TestAppCtrlShiftCCopiesSelection: Ctrl+Shift+C with an active selection
// copies it and is consumed, so the chord never leaks into the remote shell.
func TestAppCtrlShiftCCopiesSelection(t *testing.T) {
	app := New(copyTestHostList(), Deps{}, nil)
	app.TerminalPane().SetSessionForTest(copyTestKey(), "host-a", "file")

	view, backend := fedView(t)
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

	evo := app.HandleGlobalKey(tcell.NewEventKey(tcell.KeyCtrlC, 'C', tcell.ModCtrl|tcell.ModShift))
	if evo != nil {
		t.Fatalf("Ctrl+Shift+C with selection: event = %v, want nil (consumed for copy)", evo)
	}
	if copied != "abcdefghij\nklmno" {
		t.Errorf("copied = %q, want %q", copied, "abcdefghij\nklmno")
	}
	if view.HasSelection() {
		t.Error("expected the selection to be cleared after copy")
	}
	if writes := backend.Writes(); len(writes) != 0 {
		t.Errorf("Ctrl+Shift+C must not reach the remote; got %q", writes)
	}
}

// TestAppCtrlShiftCNoSelectionConsumes: with no selection the chord is still
// consumed — never SIGINT, never a literal "c" in the remote shell.
func TestAppCtrlShiftCNoSelectionConsumes(t *testing.T) {
	for _, tc := range []struct {
		name  string
		event *tcell.EventKey
	}{
		{"named-key", tcell.NewEventKey(tcell.KeyCtrlC, 'C', tcell.ModCtrl|tcell.ModShift)},
		{"kitty-lower", tcell.NewEventKey(tcell.KeyRune, 'c', tcell.ModCtrl|tcell.ModShift)},
		{"kitty-upper", tcell.NewEventKey(tcell.KeyRune, 'C', tcell.ModCtrl|tcell.ModShift)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := New(copyTestHostList(), Deps{}, nil)
			app.TerminalPane().SetSessionForTest(copyTestKey(), "host-a", "file")

			view, backend := fedView(t)
			app.TerminalPane().SetSessionViewForTest(copyTestKey(), view)
			app.FocusTerminal()

			if evo := app.HandleGlobalKey(tc.event); evo != nil {
				t.Fatalf("event = %v, want nil (consumed, not forwarded)", evo)
			}
			if writes := backend.Writes(); len(writes) != 0 {
				t.Errorf("Ctrl+Shift+C must not reach the remote; got %q", writes)
			}
		})
	}
}

// TestAppPlainCtrlCStillSIGINT: the intercept above must not swallow the plain
// Ctrl+C dual-use path — without a selection it is still forwarded as SIGINT.
func TestAppPlainCtrlCStillSIGINT(t *testing.T) {
	app := New(copyTestHostList(), Deps{}, nil)
	app.TerminalPane().SetSessionForTest(copyTestKey(), "host-a", "file")

	view, _ := fedView(t)
	app.TerminalPane().SetSessionViewForTest(copyTestKey(), view)
	app.FocusTerminal()

	if evo := app.HandleGlobalKey(tcell.NewEventKey(tcell.KeyCtrlC, 'c', tcell.ModCtrl)); evo == nil {
		t.Fatal("plain Ctrl+C without a selection must still be forwarded as SIGINT")
	}
}

// TestCtrlShiftCRealRoutingNoBytes: the whole point of the fix, exercised
// through the real tview event loop with a backend attached. Before the fix the
// chord escaped the app capture and landed in the PTY as a literal "c".
func TestCtrlShiftCRealRoutingNoBytes(t *testing.T) {
	view, backend := fedView(t)
	defer view.Close()

	app := New(copyTestHostList(), Deps{}, nil)
	app.SessionHeader().SetDialFuncForTest(func(host, port string) (int, bool) { return 1, true })
	key := SessionKey("host-a", "file")
	app.TerminalPane().SetSessionForTest(key, "host-a", "file")
	app.TerminalPane().SetSessionViewForTest(key, view)
	app.TerminalPane().Activate(key)
	app.ShowTerminalPaneForTest()

	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatalf("init simulation screen: %v", err)
	}
	screen.SetSize(80, 24)
	app.SetScreenForTest(screen)

	errCh := make(chan error, 1)
	go func() { errCh <- app.Run() }()
	defer func() {
		app.StopForTest()
		if err := <-errCh; err != nil {
			t.Errorf("app.Run: %v", err)
		}
		screen.Fini()
	}()

	// Wait for the first draw so focus and layout are settled.
	ready := make(chan struct{})
	app.app.QueueUpdateDraw(func() { close(ready) })
	select {
	case <-ready:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for the first draw")
	}

	before := len(backend.Writes())
	screen.InjectKey(tcell.KeyCtrlC, 'C', tcell.ModCtrl|tcell.ModShift)
	// Ctrl+C is then typed for real: if the chord had leaked, the "c" would
	// already be in the PTY before this point.
	time.Sleep(200 * time.Millisecond)

	if after := backend.Writes(); len(after) != before {
		t.Fatalf("Ctrl+Shift+C reached the PTY: %q", after[before:])
	}
}
