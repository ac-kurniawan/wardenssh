package tviewui

import (
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/ac-kurniawan/wardenssh/internal/hosts"
)

// TestAppScrollKeysReachTheTerminalView drives Shift+PgUp through the real tview
// event loop. Input capture runs before the focused widget, so this is what
// proves the app does not swallow the scroll keys the pane handles: pressing
// them must move the pane's history and send nothing to the remote.
func TestAppScrollKeysReachTheTerminalView(t *testing.T) {
	view, backend := fedView(t)
	defer view.Close()

	app := New(hosts.NewList(nil), Deps{}, nil)
	key := SessionKey("host-a", "file")
	app.termPane.SetSessionForTest(key, "host-a", "file")
	app.termPane.SetSessionViewForTest(key, view)
	app.termPane.Activate(key)
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
	}()

	ready := make(chan struct{})
	app.app.QueueUpdateDraw(func() {
		_, _, _, _ = view.GetInnerRect()
		close(ready)
	})
	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the first draw")
	}

	// tview runs its event loop and its update queue as separate goroutines, so
	// a QueueUpdate barrier does not prove the key was handled. Poll for the
	// effect instead: that is the observable outcome either way.
	before, _ := view.ScrollbackStatus()
	app.app.QueueEvent(tcell.NewEventKey(tcell.KeyPgUp, 0, tcell.ModShift))
	waitFor(t, "Shift+PgUp to scroll the pane", func() bool {
		after, _ := view.ScrollbackStatus()
		return after > before
	})
	if got := view.TerminalTitle(); !strings.Contains(got, "[↑ ") {
		t.Fatalf("title through the app = %q, want a scroll marker", got)
	}
	if writes := backend.Writes(); len(writes) != 0 {
		t.Errorf("scroll keys must not reach the remote, got %d writes: %q", len(writes), writes)
	}

	// A plain PgDn is the remote's key and must still be forwarded, and a
	// forwarded key snaps the view back to the live output.
	beforeWrites := len(backend.Writes())
	app.app.QueueEvent(tcell.NewEventKey(tcell.KeyPgDn, 0, tcell.ModNone))
	waitFor(t, "plain PgDn to reach the remote", func() bool {
		return len(backend.Writes()) > beforeWrites
	})
	if offset, _ := view.ScrollbackStatus(); offset != 0 {
		t.Errorf("a forwarded key must snap the view back to the bottom, offset=%d", offset)
	}
}

// waitFor polls until cond holds, failing the test if it never does.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}
