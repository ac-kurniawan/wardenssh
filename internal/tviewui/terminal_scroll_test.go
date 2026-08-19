package tviewui

import (
	"io"
	"sync"
	"testing"
	"time"

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
func waitScrollback(t *testing.T, view *terminalView) {
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

// fedView builds a terminal view with the WardenSSH interaction wiring, a rect
// (inner area 10x3), and fed output. Exactly 26 lines are written so the first
// three lines sit in scrollback and the visible rows start at
// "abcdefghij", "klmnopqrst", "uvwxyzabcd". It returns the view and the
// recording backend.
func fedView(t *testing.T) (*terminalView, *testBackend) {
	t.Helper()
	view := newTerminalView(nil, "host-a")
	view.SetRect(0, 0, 12, 5)

	payload := []byte(mouseEnable)
	for i := 0; i < 3; i++ {
		payload = append(payload, []byte("xxxxxxxxxx\n")...)
	}
	payload = append(payload, []byte("abcdefghij\n")...)
	payload = append(payload, []byte("klmnopqrst\n")...)
	payload = append(payload, []byte("uvwxyzabcd\n")...)
	for i := 0; i < 20; i++ {
		payload = append(payload, []byte("yyyyyyyyyy\n")...)
	}
	backend := newTestBackend(payload)
	view.Attach(backend)
	waitScrollback(t, view)
	return view, backend
}

// TestTerminalWheelScrollsLocalScrollback: the right pane (terminal) must be
// scrollable with the mouse wheel — wheel-up/down scrolls the pane's own
// scrollback. Wheel events must never be forwarded to the remote app as
// mouse-reporting sequences, which the remote interprets as arrow-key
// navigation ("acts like arrow up and down").
func TestTerminalWheelScrollsLocalScrollback(t *testing.T) {
	view := newTerminalView(nil, "host-a")
	view.SetRect(0, 0, 12, 5)
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

	consumed, _ := handler(tview.MouseScrollUp, tcell.NewEventMouse(5, 2, 0, tcell.ModNone), setFocus)
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

	consumed, _ = handler(tview.MouseScrollDown, tcell.NewEventMouse(5, 2, 0, tcell.ModNone), setFocus)
	if !consumed {
		t.Fatal("expected wheel-down over the terminal to be consumed")
	}
	offsetDown, _ := view.ScrollbackStatus()
	if offsetDown >= offset {
		t.Errorf("expected wheel-down to scroll toward the bottom, offset %d -> %d", offset, offsetDown)
	}
}

// TestTerminalWheelAccumulatesWithFocusReporting: repeated wheel-up events must
// keep accumulating the local scroll offset. A remote app that has enabled
// focus reporting (DEC 1004, used by vim/tmux/less) triggers a focus-in report
// whenever the view is (re)focused. Because setFocus(s) on every wheel event
// re-invokes Focus() -> reportFocus -> sendInput -> resetScrollback, the offset
// was reset to 0 before ScrollbackUp could accumulate — only the last +3 ever
// stuck, so scrolling was capped at one step. The handler must not re-focus an
// already-focused view.
func TestTerminalWheelAccumulatesWithFocusReporting(t *testing.T) {
	view := newTerminalView(nil, "host-a")
	view.SetRect(0, 0, 12, 5)
	defer view.Close()

	// Enable focus reporting the way vim/tmux would.
	payload := []byte("\x1b[?1004h")
	for i := 0; i < 60; i++ {
		payload = append(payload, []byte("line of output\n")...)
	}
	backend := newTestBackend(payload)
	view.Attach(backend)
	waitScrollback(t, view)

	handler := view.MouseHandler()
	// Realistic setFocus: simulate tview.Application.SetFocus by invoking the
	// primitive's Focus() -> reportFocus path, which fires a focus-in report to
	// the backend and (critically) calls resetScrollback via sendInput.
	setFocus := func(p tview.Primitive) {
		if p == nil {
			return
		}
		if tv, ok := p.(*terminalView); ok {
			tv.Focus(func(q tview.Primitive) {})
		}
	}

	// First scroll establishes an offset.
	handler(tview.MouseScrollUp, tcell.NewEventMouse(5, 2, 0, tcell.ModNone), setFocus)
	off1, _ := view.ScrollbackStatus()
	if off1 <= 0 {
		t.Fatalf("expected first wheel-up to set offset >0, got %d", off1)
	}

	// Second scroll must accumulate, not reset+set to one step.
	handler(tview.MouseScrollUp, tcell.NewEventMouse(5, 2, 0, tcell.ModNone), setFocus)
	off2, _ := view.ScrollbackStatus()
	if off2 <= off1 {
		t.Fatalf("expected second wheel-up to accumulate offset %d -> >%d, got %d", off1, off1, off2)
	}

	// Third scroll keeps going.
	handler(tview.MouseScrollUp, tcell.NewEventMouse(5, 2, 0, tcell.ModNone), setFocus)
	off3, _ := view.ScrollbackStatus()
	if off3 <= off2 {
		t.Fatalf("expected third wheel-up to accumulate offset %d -> >%d, got %d", off2, off2, off3)
	}
}

// TestTerminalClickKeepsScrollOffsetWithFocusReporting: clicking into the
// terminal while scrolled up (e.g. to start selecting scrollback text) must
// not snap the view back to the bottom. With a remote app that has enabled
// focus reporting (DEC 1004, used by vim/tmux/less), tview's SetFocus ->
// Focus() -> reportFocus -> sendInput -> resetScrollback fires on every
// unguarded setFocus call, wiping the scroll offset — the same mechanism as
// the wheel-accumulation bug above. MouseLeftDown must not re-focus an
// already-focused view.
func TestTerminalClickKeepsScrollOffsetWithFocusReporting(t *testing.T) {
	view := newTerminalView(nil, "host-a")
	view.SetRect(0, 0, 12, 5)
	defer view.Close()

	// Enable focus reporting the way vim/tmux would.
	payload := []byte("\x1b[?1004h")
	for i := 0; i < 60; i++ {
		payload = append(payload, []byte("line of output\n")...)
	}
	backend := newTestBackend(payload)
	view.Attach(backend)
	waitScrollback(t, view)

	handler := view.MouseHandler()
	// Realistic setFocus: simulate tview.Application.SetFocus by invoking the
	// primitive's Focus() -> reportFocus path (see the wheel test above).
	setFocus := func(p tview.Primitive) {
		if p == nil {
			return
		}
		if tv, ok := p.(*terminalView); ok {
			tv.Focus(func(q tview.Primitive) {})
		}
	}

	// Scroll up into the scrollback.
	handler(tview.MouseScrollUp, tcell.NewEventMouse(5, 2, 0, tcell.ModNone), setFocus)
	before, _ := view.ScrollbackStatus()
	if before <= 0 {
		t.Fatalf("precondition: expected scroll offset >0 after wheel-up, got %d", before)
	}

	// Plain click (press + release) inside the scrolled view.
	handler(tview.MouseLeftDown, tcell.NewEventMouse(5, 2, tcell.Button1, tcell.ModNone), setFocus)
	handler(tview.MouseLeftUp, tcell.NewEventMouse(5, 2, 0, tcell.ModNone), setFocus)

	after, _ := view.ScrollbackStatus()
	if after != before {
		t.Fatalf("expected click to preserve scroll offset %d, got %d (snapped to bottom)", before, after)
	}
}

// TestTerminalDragSelectsTextAndCopies: primary-button click-hold-drag over the
// terminal selects text locally, highlights it, and copies it to the OS
// clipboard on release. The drag must never be forwarded to the remote app as
// mouse-reporting sequences.
func TestTerminalDragSelectsTextAndCopies(t *testing.T) {
	view, backend := fedView(t)
	defer view.Close()

	var copied string
	origCopy := copySelection
	copySelection = func(text string) error { copied = text; return nil }
	defer func() { copySelection = origCopy }()

	handler := view.MouseHandler()
	setFocus := func(p tview.Primitive) {}

	consumed, _ := handler(tview.MouseLeftDown, tcell.NewEventMouse(1, 1, tcell.Button1, tcell.ModNone), setFocus)
	if !consumed {
		t.Fatal("expected left-down to be consumed")
	}
	consumed, _ = handler(tview.MouseMove, tcell.NewEventMouse(5, 2, tcell.Button1, tcell.ModNone), setFocus)
	if !consumed {
		t.Fatal("expected drag move to be consumed")
	}
	consumed, _ = handler(tview.MouseLeftUp, tcell.NewEventMouse(5, 2, 0, tcell.ModNone), setFocus)
	if !consumed {
		t.Fatal("expected left-up to be consumed")
	}

	if !view.HasSelection() {
		t.Fatal("expected a selection to remain highlighted after drag")
	}
	want := "abcdefghij\nklmno"
	if got := view.SelectedText(); got != want {
		t.Errorf("SelectedText() = %q, want %q", got, want)
	}
	if copied != want {
		t.Errorf("clipboard copy = %q, want %q", copied, want)
	}
	if writes := backend.Writes(); len(writes) != 0 {
		t.Errorf("drag must not be forwarded to the remote; got %d writes: %q", len(writes), writes)
	}
}

// TestTerminalClickWithoutDragClearsSelection: a plain click (no drag) must
// clear any existing selection instead of keeping a zero-length one.
func TestTerminalClickWithoutDragClearsSelection(t *testing.T) {
	view, _ := fedView(t)
	defer view.Close()

	if !view.StartSelection(1, 1) || !view.UpdateSelection(5, 2) {
		t.Fatal("precondition: failed to create a selection")
	}
	if !view.HasSelection() {
		t.Fatal("precondition: expected an active selection")
	}

	handler := view.MouseHandler()
	setFocus := func(p tview.Primitive) {}
	handler(tview.MouseLeftDown, tcell.NewEventMouse(2, 2, tcell.Button1, tcell.ModNone), setFocus)
	handler(tview.MouseLeftUp, tcell.NewEventMouse(2, 2, 0, tcell.ModNone), setFocus)

	if view.HasSelection() {
		t.Error("expected a plain click to clear the selection")
	}
}
