package tviewui

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/ac-kurniawan/wardenssh/internal/hosts"
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

// altScreenEnable switches to the alternate screen buffer the way a
// full-screen app (vim, less, man) does. The emulator keeps no scrollback
// there, so the pane has nothing local to scroll.
const altScreenEnable = "\x1b[?1049h"

// scrollTestView builds a terminal view with 60 lines of output and a fixed
// set of remote terminal modes, then waits until the output has been consumed.
// inner mode values:
//   - "": plain shell (primary screen, no mouse reporting)
//   - "mouse": mouse reporting on (vim "set mouse=a")
//   - "alt": alternate screen, no mouse reporting (less/man, vim "set mouse=")
//   - "alt+mouse": both (vim "set mouse=a" on the alternate screen)
func scrollTestView(t *testing.T, mode string) (*terminalView, *testBackend) {
	t.Helper()
	view := newTerminalView(nil, "host-a")
	view.SetRect(0, 0, 12, 5)

	var prefix string
	switch mode {
	case "":
	case "mouse":
		prefix = mouseEnable
	case "alt":
		prefix = altScreenEnable
	case "alt+mouse":
		prefix = mouseEnable + altScreenEnable
	default:
		t.Fatalf("unknown scroll test mode %q", mode)
	}

	payload := []byte(prefix)
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
	// The alternate screen keeps no scrollback, so waiting for rows there would
	// hang: the test only needs the mode to have been applied.
	if strings.Contains(mode, "alt") {
		waitForAltScreen(t, view)
	} else {
		waitScrollback(t, view)
	}
	return view, backend
}

// waitForAltScreen polls until the view reports the remote app switched to the
// alternate screen, which also means it has consumed the fed output.
func waitForAltScreen(t *testing.T, view *terminalView) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if view.UsingAltScreen() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for the alternate screen")
}

// fedView is a view at a plain shell prompt, with the three known lines
// ("abcdefghij", "klmnopqrst", "uvwxyzabcd") still in its visible rows.
func fedView(t *testing.T) (*terminalView, *testBackend) {
	t.Helper()
	return scrollTestView(t, "")
}

// feed pushes output into the view synchronously, so a follow-up assertion
// sees the resulting terminal state. It is how a test drives output that the
// initial backend payload did not include.
func feed(t *testing.T, view *terminalView, data string) {
	t.Helper()
	if !view.Feed([]byte(data)) {
		t.Fatal("Feed: no backend attached")
	}
}

// drainWrites discards recorded backend writes so a test can assert only on
// what a specific action sends.
func drainWrites(b *testBackend) {
	b.mu.Lock()
	b.writes = nil
	b.mu.Unlock()
}

// sentBytes concatenates everything the view has written to the backend.
func sentBytes(b *testBackend) string {
	var out []byte
	for _, w := range b.Writes() {
		out = append(out, w...)
	}
	return string(out)
}

// mouseSeqs filters recorded writes down to mouse-reporting sequences
// (SGR "\x1b[<...M/m") and returns them.
func mouseSeqs(b *testBackend) []string {
	var out []string
	for _, w := range b.Writes() {
		s := string(w)
		if strings.HasPrefix(s, "\x1b[<") {
			out = append(out, s)
		}
	}
	return out
}

// TestTerminalWheelScrollsLocalScrollback: at a plain shell (no remote mouse
// reporting, no alternate screen) the wheel scrolls the pane's own scrollback
// and is never sent to the remote.
func TestTerminalWheelScrollsLocalScrollback(t *testing.T) {
	view, backend := fedView(t)
	defer view.Close()

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
		t.Errorf("wheel must not be forwarded to the remote at a plain shell; got %d writes: %q", len(writes), writes)
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

// TestTerminalWheelGoesToMouseReportingApp: when the remote app has turned on
// mouse reporting (vim "set mouse=a", htop, tmux), the wheel must be forwarded
// to the app as an SGR mouse sequence so vim scrolls its own buffer, and the
// pane's local scrollback must not move.
func TestTerminalWheelGoesToMouseReportingApp(t *testing.T) {
	view, backend := scrollTestView(t, "mouse")
	defer view.Close()

	handler := view.MouseHandler()
	setFocus := func(p tview.Primitive) {}

	if consumed, _ := handler(tview.MouseScrollUp, tcell.NewEventMouse(2, 2, 0, tcell.ModNone), setFocus); !consumed {
		t.Fatal("expected wheel-up over the terminal to be consumed")
	}
	seqs := mouseSeqs(backend)
	if len(seqs) != 1 {
		t.Fatalf("expected the wheel to be sent to the app as one mouse sequence, got %d: %q", len(seqs), seqs)
	}
	want := "\x1b[<64;2;2M"
	if seqs[0] != want {
		t.Errorf("wheel-up sequence = %q, want %q", seqs[0], want)
	}
	if offset, _ := view.ScrollbackStatus(); offset != 0 {
		t.Errorf("local scrollback must not move while the app owns the mouse, offset=%d", offset)
	}

	drainWrites(backend)
	if consumed, _ := handler(tview.MouseScrollDown, tcell.NewEventMouse(2, 2, 0, tcell.ModNone), setFocus); !consumed {
		t.Fatal("expected wheel-down over the terminal to be consumed")
	}
	seqs = mouseSeqs(backend)
	if len(seqs) != 1 {
		t.Fatalf("expected wheel-down to be sent to the app as one mouse sequence, got %d: %q", len(seqs), seqs)
	}
	if want := "\x1b[<65;2;2M"; seqs[0] != want {
		t.Errorf("wheel-down sequence = %q, want %q", seqs[0], want)
	}
}

// TestTerminalWheelOnAltScreenSendsArrows: on the alternate screen with no
// mouse reporting (less, man, vim "set mouse=") there is no pane history to
// scroll, so each wheel tick must send the app Up/Down arrow keys. The
// application must get three presses per tick, matching gnome-terminal/iTerm.
func TestTerminalWheelOnAltScreenSendsArrows(t *testing.T) {
	view, backend := scrollTestView(t, "alt")
	defer view.Close()

	handler := view.MouseHandler()
	setFocus := func(p tview.Primitive) {}

	if consumed, _ := handler(tview.MouseScrollUp, tcell.NewEventMouse(2, 2, 0, tcell.ModNone), setFocus); !consumed {
		t.Fatal("expected wheel-up over the terminal to be consumed")
	}
	if got := sentBytes(backend); got != "\x1b[A\x1b[A\x1b[A" {
		t.Errorf("wheel-up on the alternate screen sent %q, want three Up arrows", got)
	}
	if offset, _ := view.ScrollbackStatus(); offset != 0 {
		t.Errorf("alternate screen has no local scrollback, offset=%d", offset)
	}

	drainWrites(backend)
	if consumed, _ := handler(tview.MouseScrollDown, tcell.NewEventMouse(2, 2, 0, tcell.ModNone), setFocus); !consumed {
		t.Fatal("expected wheel-down over the terminal to be consumed")
	}
	if got := sentBytes(backend); got != "\x1b[B\x1b[B\x1b[B" {
		t.Errorf("wheel-down on the alternate screen sent %q, want three Down arrows", got)
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

// TestTerminalDragPastEdgeAutoScrolls: dragging a selection off the bottom of
// the terminal (pointer held, no wheel) must scroll the local scrollback so
// more text can be selected. The drag must not be forwarded to the remote.
func TestTerminalDragPastEdgeAutoScrolls(t *testing.T) {
	view, backend := fedView(t)
	defer view.Close()
	drawTerminal(t, view)

	handler := view.MouseHandler()
	setFocus := func(p tview.Primitive) {}

	// Leave the live row so a downward drag has somewhere to scroll.
	view.ScrollbackUp(6)
	beforeDown, _ := view.ScrollbackStatus()
	if beforeDown <= 0 {
		t.Fatal("precondition: expected scrollback offset > 0")
	}

	if consumed, _ := handler(tview.MouseLeftDown, tcell.NewEventMouse(1, 2, tcell.Button1, tcell.ModNone), setFocus); !consumed {
		t.Fatal("expected left-down to be consumed")
	}
	// Inner area is y=1..3 (rect 0,0 12x5 with a border). y=6 is past the
	// bottom edge while the button is still held.
	if consumed, _ := handler(tview.MouseMove, tcell.NewEventMouse(1, 6, tcell.Button1, tcell.ModNone), setFocus); !consumed {
		t.Fatal("expected drag past the bottom edge to be consumed")
	}
	afterDown, _ := view.ScrollbackStatus()
	if afterDown >= beforeDown {
		t.Errorf("expected drag past the bottom to scroll toward newer lines, offset %d -> %d", beforeDown, afterDown)
	}
	// Keep dragging past the bottom until the live row is back.
	before, _ := view.ScrollbackStatus()
	for i := 0; i < 10 && before != 0; i++ {
		handler(tview.MouseMove, tcell.NewEventMouse(1, 6, tcell.Button1, tcell.ModNone), setFocus)
		before, _ = view.ScrollbackStatus()
	}
	if before != 0 {
		t.Fatalf("precondition: expected drag-down to reach the bottom, offset=%d", before)
	}
	handler(tview.MouseLeftUp, tcell.NewEventMouse(1, 6, 0, tcell.ModNone), setFocus)
	handler(tview.MouseLeftDown, tcell.NewEventMouse(1, 2, tcell.Button1, tcell.ModNone), setFocus)
	// y=0 is above the inner area (border row).
	if consumed, _ := handler(tview.MouseMove, tcell.NewEventMouse(1, 0, tcell.Button1, tcell.ModNone), setFocus); !consumed {
		t.Fatal("expected drag past the top edge to be consumed")
	}
	after, _ := view.ScrollbackStatus()
	if after <= before {
		t.Errorf("expected drag past the top to scroll toward older lines, offset %d -> %d", before, after)
	}
	if writes := backend.Writes(); len(writes) != 0 {
		t.Errorf("edge drag must not be forwarded to the remote; got %d writes: %q", len(writes), writes)
	}
}

// TestTerminalWheelDuringDragKeepsSelection: scrolling the wheel while a
// selection drag is held must move the local scrollback and keep (extend) the
// highlight. It must not reset the selection.
func TestTerminalWheelDuringDragKeepsSelection(t *testing.T) {
	view, backend := fedView(t)
	defer view.Close()
	drawTerminal(t, view)

	handler := view.MouseHandler()
	setFocus := func(p tview.Primitive) {}

	handler(tview.MouseLeftDown, tcell.NewEventMouse(1, 1, tcell.Button1, tcell.ModNone), setFocus)
	handler(tview.MouseMove, tcell.NewEventMouse(5, 2, tcell.Button1, tcell.ModNone), setFocus)
	if !view.HasSelection() {
		t.Fatal("precondition: expected a selection after drag")
	}
	before := view.SelectedText()

	consumed, _ := handler(tview.MouseScrollUp, tcell.NewEventMouse(5, 2, tcell.Button1, tcell.ModNone), setFocus)
	if !consumed {
		t.Fatal("expected wheel-up during drag to be consumed")
	}
	offset, _ := view.ScrollbackStatus()
	if offset <= 0 {
		t.Errorf("expected wheel-up during drag to scroll local scrollback, offset=%d", offset)
	}
	if !view.HasSelection() {
		t.Fatal("wheel during drag must not clear the selection")
	}
	if got := view.SelectedText(); got == "" {
		t.Error("expected selected text to survive the wheel scroll")
	} else if got == before {
		t.Errorf("expected the selection to extend into the newly revealed lines, still %q", got)
	}

	if writes := backend.Writes(); len(writes) != 0 {
		t.Errorf("wheel during drag must not be forwarded to the remote; got %d writes: %q", len(writes), writes)
	}
}

// drawTerminal sizes the emulator to the view's inner rect. Attach alone leaves
// the default 80x24 grid; Draw is what applies SetRect.
func drawTerminal(t *testing.T, view *terminalView) {
	t.Helper()
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatalf("init simulation screen: %v", err)
	}
	defer screen.Fini()
	view.Draw(screen)
}

// TestAppDragOffScreenEdgeAutoScrolls drives the drag through the real tview
// event loop and layout. Dragging to the last row of the screen (the pointer
// has left the terminal primitive) must still scroll the local scrollback.
// Calling the terminal handler directly hides this: Pages and Flex drop mouse
// events once the pointer leaves their rectangle, so the drag never arrives.
func TestAppDragOffScreenEdgeAutoScrolls(t *testing.T) {
	view, _ := fedView(t)
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
	var clickY, dragY, iy, ih int
	ready := make(chan struct{})
	app.app.QueueUpdateDraw(func() {
		view.ScrollbackUp(6)
		_, iy, _, ih = view.GetInnerRect()
		_, height := screen.Size()
		clickY = iy + 1
		dragY = height - 1
		close(ready)
	})
	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the first draw")
	}
	before, _ := view.ScrollbackStatus()
	if before <= 0 {
		t.Fatal("precondition: expected scrollback offset > 0")
	}
	if ih <= 0 {
		t.Fatal("precondition: terminal has no inner rows after layout")
	}

	waitEvent := func(ev tcell.Event) {
		t.Helper()
		done := make(chan struct{})
		app.app.QueueEvent(ev)
		app.app.QueueUpdate(func() { close(done) })
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for a mouse event")
		}
	}

	// Press inside the terminal, then drag onto the last row of the screen.
	// That row belongs to the footer, outside the terminal primitive.
	waitEvent(tcell.NewEventMouse(40, clickY, tcell.Button1, tcell.ModNone))
	if !view.Dragging() {
		x, y, w, h := view.GetRect()
		t.Fatalf("precondition: mouse down at y=%d did not start a drag (rect %d,%d %dx%d)", clickY, x, y, w, h)
	}
	waitEvent(tcell.NewEventMouse(40, dragY, tcell.Button1, tcell.ModNone))

	after, _ := view.ScrollbackStatus()
	if after >= before {
		t.Errorf("dragging onto the last screen row must scroll toward newer lines, offset %d -> %d", before, after)
	}
}

// TestAppWheelDuringDragKeepsSelection drives a wheel event through the real
// event loop while a selection drag is held. The highlight must survive and
// the local scrollback must move.
func TestAppWheelDuringDragKeepsSelection(t *testing.T) {
	view, _ := fedView(t)
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

	var clickY int
	ready := make(chan struct{})
	app.app.QueueUpdateDraw(func() {
		_, y, _, _ := view.GetInnerRect()
		clickY = y + 1
		close(ready)
	})
	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the first draw")
	}

	waitEvent := func(ev tcell.Event) {
		t.Helper()
		done := make(chan struct{})
		app.app.QueueEvent(ev)
		app.app.QueueUpdate(func() { close(done) })
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for a mouse event")
		}
	}

	waitEvent(tcell.NewEventMouse(40, clickY, tcell.Button1, tcell.ModNone))
	waitEvent(tcell.NewEventMouse(44, clickY+1, tcell.Button1, tcell.ModNone))
	if !view.HasSelection() {
		t.Fatal("precondition: expected a selection after the drag")
	}

	waitEvent(tcell.NewEventMouse(44, clickY+1, tcell.WheelUp, tcell.ModNone))
	if !view.HasSelection() {
		t.Fatal("wheel during a drag must not clear the selection")
	}
	offset, _ := view.ScrollbackStatus()
	if offset <= 0 {
		t.Errorf("wheel during a drag must scroll the local scrollback, offset=%d", offset)
	}
}

// TestTerminalTitleShowsScrollPosition: while scrolled up the pane title must
// say so, with the number of lines above the live output. The session chrome
// rewrites the title on its 1s tick, so the marker has to be part of the title
// the view owns rather than a one-shot SetTitle.
func TestTerminalTitleShowsScrollPosition(t *testing.T) {
	view, _ := fedView(t)
	defer view.Close()

	base := view.TerminalTitle()
	if base != "host-a" {
		t.Fatalf("precondition: unexpected base title %q", base)
	}
	if strings.Contains(base, "↑") {
		t.Fatalf("expected no scroll marker at the bottom, got %q", base)
	}

	view.ScrollbackUp(4)
	offset, _ := view.ScrollbackStatus()
	if offset == 0 {
		t.Fatal("precondition: expected the view to be scrolled up")
	}
	want := fmt.Sprintf("[↑ %d]", offset)
	if got := view.TerminalTitle(); !strings.Contains(got, want) {
		t.Fatalf("scrolled title = %q, want it to contain %q", got, want)
	}

	view.ScrollbackDown(offset)
	if got := view.TerminalTitle(); strings.Contains(got, "↑") {
		t.Fatalf("title after returning to the bottom = %q, want no scroll marker", got)
	}
}

// TestTerminalTitleMarkerSurvivesSessionChrome: the pane's session chrome sets
// the full title (uptime, ping, state) and must keep the scroll marker, or the
// marker would vanish on the next uptime tick.
func TestTerminalTitleMarkerSurvivesSessionChrome(t *testing.T) {
	view, _ := fedView(t)
	defer view.Close()

	pane := NewTerminalPane(nil)
	defer pane.Close()
	key := SessionKey("host-a", "file")
	pane.SetSessionForTest(key, "host-a", "file")
	pane.SetSessionViewForTest(key, view)
	pane.Activate(key)

	view.ScrollbackUp(7)
	offset, _ := view.ScrollbackStatus()
	if offset == 0 {
		t.Fatal("precondition: expected the view to be scrolled up")
	}
	want := fmt.Sprintf("[↑ %d]", offset)
	pane.SetSessionTitleState(true)

	if got := view.TerminalTitle(); !strings.Contains(got, want) {
		t.Fatalf("title after the chrome rewrite = %q, want it to still contain %q", got, want)
	}
	if got := pane.ActiveTitle(); !strings.Contains(got, want) {
		t.Fatalf("ActiveTitle() = %q, want it to report the scroll marker", got)
	}
}

// TestTerminalScrollKeysScrollLocally: Shift+PgUp/PgDn/Home/End move the pane's
// own history and must not reach the remote app. These are the keyboard
// equivalents of the wheel.
func TestTerminalScrollKeysScrollLocally(t *testing.T) {
	view, backend := fedView(t)
	defer view.Close()

	view.SendKey(tcell.NewEventKey(tcell.KeyPgUp, 0, tcell.ModShift))
	offset, _ := view.ScrollbackStatus()
	if offset <= 0 {
		t.Fatalf("expected Shift+PgUp to scroll the local scrollback, offset=%d", offset)
	}
	view.SendKey(tcell.NewEventKey(tcell.KeyHome, 0, tcell.ModShift))
	top, rows := view.ScrollbackStatus()
	if top != rows {
		t.Fatalf("expected Shift+Home to jump to the oldest line, offset=%d rows=%d", top, rows)
	}
	view.SendKey(tcell.NewEventKey(tcell.KeyEnd, 0, tcell.ModShift))
	if offset, _ := view.ScrollbackStatus(); offset != 0 {
		t.Fatalf("expected Shift+End to return to the bottom, offset=%d", offset)
	}
	if writes := backend.Writes(); len(writes) != 0 {
		t.Fatalf("scroll keys must not reach the remote, got %d writes: %q", len(writes), writes)
	}
}

// TestTerminalPlainKeysResetScroll: a key the remote receives means the user is
// typing again, so the view returns to the live output.
func TestTerminalPlainKeysResetScroll(t *testing.T) {
	view, backend := fedView(t)
	defer view.Close()

	view.ScrollbackUp(5)
	if offset, _ := view.ScrollbackStatus(); offset == 0 {
		t.Fatal("precondition: expected a scrolled view")
	}
	view.SendKey(tcell.NewEventKey(tcell.KeyPgDn, 0, tcell.ModNone))
	if offset, _ := view.ScrollbackStatus(); offset != 0 {
		t.Fatalf("expected a forwarded key to snap the view back to the bottom, offset=%d", offset)
	}
	if writes := backend.Writes(); len(writes) == 0 {
		t.Fatal("precondition: expected the key to reach the remote")
	}
}

// TestTerminalWheelDuringDragOnMouseAppKeepsSelection: on an app that owns the
// mouse, a wheel tick with a selection drag held still extends the local
// selection rather than being forwarded, so a drag in progress is never
// interrupted by its own scrolling.
func TestTerminalWheelDuringDragOnMouseAppKeepsSelection(t *testing.T) {
	view, backend := scrollTestView(t, "mouse")
	defer view.Close()
	drawTerminal(t, view)

	handler := view.MouseHandler()
	setFocus := func(p tview.Primitive) {}

	handler(tview.MouseLeftDown, tcell.NewEventMouse(1, 1, tcell.Button1, tcell.ModNone), setFocus)
	handler(tview.MouseMove, tcell.NewEventMouse(5, 2, tcell.Button1, tcell.ModNone), setFocus)
	if !view.HasSelection() {
		t.Fatal("precondition: expected a selection after the drag")
	}
	before := len(mouseSeqs(backend))

	handler(tview.MouseScrollUp, tcell.NewEventMouse(5, 2, tcell.Button1, tcell.ModNone), setFocus)
	if !view.HasSelection() {
		t.Fatal("wheel during a drag must not clear the selection")
	}
	if after := len(mouseSeqs(backend)); after != before {
		t.Fatalf("wheel during a selection drag must stay local, got %d new mouse sequences", after-before)
	}
}

// TestTerminalTitleMarkerTracksNewOutput: while scrolled up, new output pushes
// the view deeper into history (the anchor keeps the content still). The marker
// must follow, or it would report a stale distance.
func TestTerminalTitleMarkerTracksNewOutput(t *testing.T) {
	view, _ := fedView(t)
	defer view.Close()

	view.ScrollbackUp(4)
	before, _ := view.ScrollbackStatus()
	if before == 0 {
		t.Fatal("precondition: expected the view to be scrolled up")
	}

	feed(t, view, "more output\nmore output\n")
	after, _ := view.ScrollbackStatus()
	if after == before {
		t.Fatalf("precondition: expected the anchor to push the view deeper, still %d", after)
	}
	if got := view.TerminalTitle(); !strings.Contains(got, fmt.Sprintf("[↑ %d]", after)) {
		t.Fatalf("title after new output = %q, want it to report %d lines above live", got, after)
	}
}

// TestTerminalTitleMarkerClearsOnTyping: a key forwarded to the remote snaps the
// view back to the live output, so the marker must go with it.
func TestTerminalTitleMarkerClearsOnTyping(t *testing.T) {
	view, _ := fedView(t)
	defer view.Close()

	view.ScrollbackUp(4)
	if got := view.TerminalTitle(); !strings.Contains(got, "↑") {
		t.Fatalf("precondition: expected a scroll marker, got %q", got)
	}

	view.SendKey(tcell.NewEventKey(tcell.KeyRune, 'x', tcell.ModNone))
	if got := view.TerminalTitle(); strings.Contains(got, "↑") {
		t.Fatalf("title after typing = %q, want no scroll marker", got)
	}
}

// TestTerminalNoPhantomMarkerOnAltScreen: when the remote app switches to the
// alternate screen while the pane is scrolled up, the emulator drops the
// scrollback. A stale offset would leave the title claiming "[↑ N]" over a live
// full-screen app, so the marker must disappear with the history it measured.
func TestTerminalNoPhantomMarkerOnAltScreen(t *testing.T) {
	view, _ := fedView(t)
	defer view.Close()

	view.ScrollbackUp(5)
	if offset, rows := view.ScrollbackStatus(); offset == 0 || rows == 0 {
		t.Fatalf("precondition: expected a scrolled view with history, offset=%d rows=%d", offset, rows)
	}

	feed(t, view, altScreenEnable)

	offset, rows := view.ScrollbackStatus()
	if rows != 0 {
		t.Fatalf("precondition: the alternate screen should have no history, rows=%d", rows)
	}
	if offset != 0 {
		t.Errorf("alternate screen kept a stale scroll offset %d with no history", offset)
	}
	if got := view.TerminalTitle(); strings.Contains(got, "↑") {
		t.Errorf("title over a live full-screen app = %q, want no scroll marker", got)
	}
	if start := firstVisibleRowOf(view); start != 0 {
		t.Errorf("expected the live screen to be shown, first visible row=%d", start)
	}
}

// TestTerminalDrawnTitleShowsMarker: the marker has to reach the border the user
// reads, not just the accessor. Rendering the pane into a simulation screen and
// reading the title row is what pins the visible contract.
func TestTerminalDrawnTitleShowsMarker(t *testing.T) {
	view, _ := fedView(t)
	defer view.Close()
	// The 12-cell fixture is too narrow for a title; use a realistic pane width.
	view.SetRect(0, 0, 80, 6)

	if got := drawnTitle(t, view); !strings.Contains(got, "host-a") || strings.Contains(got, "↑") {
		t.Fatalf("drawn title at the bottom = %q, want the plain session title", got)
	}

	view.ScrollbackUp(4)
	offset, _ := view.ScrollbackStatus()
	want := fmt.Sprintf("[↑ %d]", offset)
	if got := drawnTitle(t, view); !strings.Contains(got, want) {
		t.Fatalf("drawn title while scrolled = %q, want it to contain %q", got, want)
	}

	view.ScrollbackBottom()
	if got := drawnTitle(t, view); strings.Contains(got, "↑") {
		t.Fatalf("drawn title after returning to the bottom = %q, want no marker", got)
	}
}

// drawnTitle renders the view and returns the text of its top border row, which
// is where tview draws the title.
func drawnTitle(t *testing.T, view *terminalView) string {
	t.Helper()
	x, y, width, _ := view.GetRect()
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatalf("init simulation screen: %v", err)
	}
	defer screen.Fini()
	screen.SetSize(width+x, y+6)
	view.Draw(screen)

	var b strings.Builder
	for col := 0; col < width; col++ {
		ch, _, _, _ := screen.GetContent(x+col, y)
		if ch == 0 {
			ch = ' '
		}
		b.WriteRune(ch)
	}
	return b.String()
}

// firstVisibleRowOf is the wrapper-level counterpart of firstVisibleRow: with no
// retained history the live screen starts at row 0, which is all this test needs.
func firstVisibleRowOf(v *terminalView) int {
	offset, rows := v.ScrollbackStatus()
	if rows == 0 {
		return 0
	}
	return max(0, rows-offset)
}
