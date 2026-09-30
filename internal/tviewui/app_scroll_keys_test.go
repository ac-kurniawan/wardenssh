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

	waitKey := func(ev tcell.Event) {
		t.Helper()
		done := make(chan struct{})
		app.app.QueueEvent(ev)
		app.app.QueueUpdate(func() { close(done) })
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for a key event")
		}
	}

	before, _ := view.ScrollbackStatus()
	waitKey(tcell.NewEventKey(tcell.KeyPgUp, 0, tcell.ModShift))
	after, _ := view.ScrollbackStatus()
	if after <= before {
		t.Fatalf("Shift+PgUp through the app did not scroll the pane: offset %d -> %d", before, after)
	}
	if got := view.GetTitle(); !strings.Contains(got, "[↑ ") {
		t.Fatalf("title through the app = %q, want a scroll marker", got)
	}
	if writes := backend.Writes(); len(writes) != 0 {
		t.Errorf("scroll keys must not reach the remote, got %d writes: %q", len(writes), writes)
	}

	// A plain PgUp is the remote's key and must still be forwarded.
	beforeWrites := len(backend.Writes())
	waitKey(tcell.NewEventKey(tcell.KeyPgDn, 0, tcell.ModNone))
	if got := len(backend.Writes()); got == beforeWrites {
		t.Error("plain PgDn through the app did not reach the remote")
	}
	if offset, _ := view.ScrollbackStatus(); offset != 0 {
		t.Errorf("a forwarded key must snap the view back to the bottom, offset=%d", offset)
	}
}
