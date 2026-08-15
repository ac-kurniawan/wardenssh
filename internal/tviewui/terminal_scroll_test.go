package tviewui

import (
	"io"
	"sync"
	"testing"
	"time"

	"github.com/blacknon/tvxterm"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// testBackend delivers queued chunks to a tvxterm.View and records everything
// the view writes back (what would go to the remote app).
type testBackend struct {
	data   chan []byte
	mu     sync.Mutex
	writes [][]byte
}

func newTestBackend(chunks ...[]byte) *testBackend {
	b := &testBackend{data: make(chan []byte, len(chunks))}
	for _, c := range chunks {
		b.data <- c
	}
	return b
}

func (b *testBackend) Read(p []byte) (int, error) {
	chunk, ok := <-b.data
	if !ok {
		return 0, io.EOF
	}
	return copy(p, chunk), nil
}

func (b *testBackend) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.writes = append(b.writes, append([]byte(nil), p...))
	return len(p), nil
}

func (b *testBackend) Resize(cols, rows int) error { return nil }

func (b *testBackend) Close() error {
	close(b.data)
	return nil
}

func (b *testBackend) Writes() [][]byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([][]byte(nil), b.writes...)
}

// waitScrollback polls until the view's local scrollback is non-empty, which
// also guarantees the view has fully processed the fed output.
func waitScrollback(t *testing.T, view *tvxterm.View) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, rows := view.ScrollbackStatus(); rows > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	_, rows := view.ScrollbackStatus()
	t.Fatalf("timed out waiting for scrollback; rows=%d", rows)
}

// mouseEnable turns on VT200 + SGR mouse reporting the way a remote app
// (vim/tmux/less/htop) would.
const mouseEnable = "\x1b[?1000h\x1b[?1006h"

// TestTerminalWheelScrollsLocalScrollback: the right pane (terminal) must be
// scrollable with the mouse wheel — wheel-up/down scrolls the pane's own
// scrollback. Wheel events must never be forwarded to the remote app as
// mouse-reporting sequences, which the remote interprets as arrow-key
// navigation ("acts like arrow up and down").
func TestTerminalWheelScrollsLocalScrollback(t *testing.T) {
	view := newTerminalView(nil, "host-a")
	defer view.Close()

	payload := []byte(mouseEnable)
	for i := 0; i < 60; i++ {
		payload = append(payload, []byte("line of output\n")...)
	}
	backend := newTestBackend(payload)
	view.Attach(backend)
	waitScrollback(t, view)

	handler := view.MouseHandler()
	setFocus := func(p tview.Primitive) {}

	consumed, _ := handler(tview.MouseScrollUp, tcell.NewEventMouse(5, 5, 0, tcell.ModNone), setFocus)
	if !consumed {
		t.Fatal("expected wheel-up over the terminal to be consumed")
	}
	offset, rows := view.ScrollbackStatus()
	if rows == 0 {
		t.Fatal("expected the terminal to have scrollback rows")
	}
	if offset <= 0 {
		t.Errorf("expected wheel-up to scroll the local scrollback, offset=%d want >0", offset)
	}
	if writes := backend.Writes(); len(writes) != 0 {
		t.Errorf("wheel must not be forwarded to the remote as a mouse sequence; got %d writes: %q", len(writes), writes)
	}

	consumed, _ = handler(tview.MouseScrollDown, tcell.NewEventMouse(5, 5, 0, tcell.ModNone), setFocus)
	if !consumed {
		t.Fatal("expected wheel-down over the terminal to be consumed")
	}
	offsetDown, _ := view.ScrollbackStatus()
	if offsetDown >= offset {
		t.Errorf("expected wheel-down to scroll toward the bottom, offset %d -> %d", offset, offsetDown)
	}
}

// TestTerminalMouseClicksStillForwardedToRemote: only wheel scrolling is
// captured for local scrollback. Ordinary clicks must still reach the remote
// app (SGR mouse reports) so apps like tmux/vim keep working with the mouse.
func TestTerminalMouseClicksStillForwardedToRemote(t *testing.T) {
	view := newTerminalView(nil, "host-a")
	view.SetRect(0, 0, 10, 5)
	defer view.Close()

	payload := []byte(mouseEnable)
	for i := 0; i < 30; i++ {
		payload = append(payload, []byte("line\n")...)
	}
	backend := newTestBackend(payload)
	view.Attach(backend)
	waitScrollback(t, view)

	handler := view.MouseHandler()
	consumed, _ := handler(tview.MouseLeftDown, tcell.NewEventMouse(1, 1, tcell.Button1, tcell.ModNone), func(p tview.Primitive) {})
	if !consumed {
		t.Fatal("expected left-click over the terminal to be consumed")
	}
	writes := backend.Writes()
	if len(writes) != 1 || string(writes[0]) != "\x1b[<0;1;1M" {
		t.Errorf("expected left-click forwarded as SGR report, got %q", writes)
	}
}
