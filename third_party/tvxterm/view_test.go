package tvxterm

import (
	"io"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func TestKeyToBytesUsesApplicationCursorMode(t *testing.T) {
	ev := tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone)

	got, ok := keyToBytes(ev, true)
	if !ok {
		t.Fatalf("expected key to be handled")
	}
	if string(got) != "\x1bOB" {
		t.Fatalf("expected application cursor sequence, got %q", string(got))
	}
}

func TestKeyToBytesUsesNormalCursorMode(t *testing.T) {
	ev := tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone)

	got, ok := keyToBytes(ev, false)
	if !ok {
		t.Fatalf("expected key to be handled")
	}
	if string(got) != "\x1b[B" {
		t.Fatalf("expected normal cursor sequence, got %q", string(got))
	}
}

func TestKeyToBytesUsesDelForBackspace(t *testing.T) {
	// Backspace sends DEL (0x7f), the erase character modern ssh servers and
	// readline expect. tcell ≥2.10 normalizes KeyBackspace2 to KeyBackspace,
	// so a single code path serves both key names.
	ev := tcell.NewEventKey(tcell.KeyBackspace, 0, tcell.ModNone)

	got, ok := keyToBytes(ev, false)
	if !ok {
		t.Fatalf("expected backspace key to be handled")
	}
	if len(got) != 1 || got[0] != 0x7f {
		t.Fatalf("expected DEL backspace, got %q", string(got))
	}
}

func TestKeyToBytesUsesDelForBackspace2(t *testing.T) {
	// tcell ≥2.10 normalizes KeyBackspace2 to KeyBackspace in NewEventKey; the
	// distinction is unobservable through the public event API, so this now
	// asserts the delivered event's encoding (DEL, the modern default).
	ev := tcell.NewEventKey(tcell.KeyBackspace2, 0, tcell.ModNone)

	got, ok := keyToBytes(ev, false)
	if !ok {
		t.Fatalf("expected backspace2 key to be handled")
	}
	if len(got) != 1 || got[0] != 0x7f {
		t.Fatalf("expected DEL backspace, got %q", string(got))
	}
}

func TestKeyToBytesUsesCtrlA(t *testing.T) {
	ev := tcell.NewEventKey(tcell.KeyCtrlA, 0, tcell.ModNone)

	got, ok := keyToBytes(ev, false)
	if !ok {
		t.Fatalf("expected ctrl-a to be handled")
	}
	if len(got) != 1 || got[0] != 0x01 {
		t.Fatalf("expected ctrl-a byte, got %v", got)
	}
}

func TestKeyToBytesUsesCtrlZ(t *testing.T) {
	ev := tcell.NewEventKey(tcell.KeyCtrlZ, 0, tcell.ModNone)

	got, ok := keyToBytes(ev, false)
	if !ok {
		t.Fatalf("expected ctrl-z to be handled")
	}
	if len(got) != 1 || got[0] != 0x1a {
		t.Fatalf("expected ctrl-z byte, got %v", got)
	}
}

func TestKeyToBytesUsesAltBackspace(t *testing.T) {
	// tcell ≥2.10 normalizes KeyBackspace2 to KeyBackspace in NewEventKey, so
	// the event the terminal actually delivers is KeyBackspace+ModAlt.
	ev := tcell.NewEventKey(tcell.KeyBackspace, 0, tcell.ModAlt)

	got, ok := keyToBytes(ev, false)
	if !ok {
		t.Fatalf("expected alt-backspace to be handled")
	}
	want := []byte{0x1b, 0x7f}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("expected alt-backspace bytes %v, got %v", want, got)
	}
}

func TestKeyToBytesUsesAltLeftForBackwardWord(t *testing.T) {
	ev := tcell.NewEventKey(tcell.KeyLeft, 0, tcell.ModAlt)

	got, ok := keyToBytes(ev, false)
	if !ok {
		t.Fatalf("expected alt-left to be handled")
	}
	want := []byte{0x1b, 'b'}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("expected alt-left bytes %v, got %v", want, got)
	}
}

func TestKeyToBytesUsesAltRightForForwardWord(t *testing.T) {
	ev := tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModAlt)

	got, ok := keyToBytes(ev, false)
	if !ok {
		t.Fatalf("expected alt-right to be handled")
	}
	want := []byte{0x1b, 'f'}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("expected alt-right bytes %v, got %v", want, got)
	}
}

func TestKeyToBytesUsesCtrlLeft(t *testing.T) {
	ev := tcell.NewEventKey(tcell.KeyLeft, 0, tcell.ModCtrl)

	got, ok := keyToBytes(ev, false)
	if !ok {
		t.Fatalf("expected ctrl-left to be handled")
	}
	if string(got) != "\x1b[1;5D" {
		t.Fatalf("expected ctrl-left sequence, got %q", string(got))
	}
}

func TestKeyToBytesUsesHomeAndEnd(t *testing.T) {
	home, ok := keyToBytes(tcell.NewEventKey(tcell.KeyHome, 0, tcell.ModNone), false)
	if !ok || string(home) != "\x1b[H" {
		t.Fatalf("expected home sequence, got %q", string(home))
	}

	end, ok := keyToBytes(tcell.NewEventKey(tcell.KeyEnd, 0, tcell.ModNone), false)
	if !ok || string(end) != "\x1b[F" {
		t.Fatalf("expected end sequence, got %q", string(end))
	}
}

func TestKeyToBytesUsesDelete(t *testing.T) {
	ev := tcell.NewEventKey(tcell.KeyDelete, 0, tcell.ModNone)

	got, ok := keyToBytes(ev, false)
	if !ok {
		t.Fatalf("expected delete to be handled")
	}
	if string(got) != "\x1b[3~" {
		t.Fatalf("expected delete sequence, got %q", string(got))
	}
}

type shortWriter struct {
	writes [][]byte
	limit  int
}

func (w *shortWriter) Write(p []byte) (int, error) {
	n := w.limit
	if n <= 0 || n > len(p) {
		n = len(p)
	}
	chunk := make([]byte, n)
	copy(chunk, p[:n])
	w.writes = append(w.writes, chunk)
	return n, nil
}

func TestWriteAllRetriesShortWrites(t *testing.T) {
	w := &shortWriter{limit: 1}

	if err := writeAll(w, []byte("\x1b[B")); err != nil {
		t.Fatalf("expected short writes to be retried, got %v", err)
	}
	if len(w.writes) != 3 {
		t.Fatalf("expected 3 writes, got %d", len(w.writes))
	}
}

type zeroWriter struct{}

func (zeroWriter) Write(p []byte) (int, error) {
	return 0, nil
}

func TestWriteAllReturnsShortWriteOnZeroProgress(t *testing.T) {
	err := writeAll(zeroWriter{}, []byte("abc"))
	if err != io.ErrShortWrite {
		t.Fatalf("expected io.ErrShortWrite, got %v", err)
	}
}

func TestViewScrollbackPageMethodsChangeOffset(t *testing.T) {
	v := New(nil)
	v.emu = NewEmulator(4, 3)
	_, _ = v.emu.Write([]byte("1111\n2222\n3333\n4444"))

	v.ScrollbackPageUp()
	if v.scrollOffset == 0 {
		t.Fatalf("expected scroll offset to increase after page up")
	}

	v.ScrollbackPageDown()
	if v.scrollOffset != 0 {
		t.Fatalf("expected scroll offset reset after page down, got %d", v.scrollOffset)
	}
}

type stubBackend struct {
	writes [][]byte
	cols   int
	rows   int
}

func (b *stubBackend) Read(p []byte) (int, error) { return 0, io.EOF }
func (b *stubBackend) Write(p []byte) (int, error) {
	b.writes = append(b.writes, append([]byte(nil), p...))
	return len(p), nil
}
func (b *stubBackend) Resize(cols, rows int) error {
	b.cols = cols
	b.rows = rows
	return nil
}
func (b *stubBackend) Close() error { return nil }

func TestViewInputResetsScrollbackToBottom(t *testing.T) {
	v := New(nil)
	v.emu = NewEmulator(4, 3)
	_, _ = v.emu.Write([]byte("1111\n2222\n3333\n4444"))
	v.scrollOffset = 1

	backend := &stubBackend{}
	v.backend = backend

	handler := v.InputHandler()
	handler(tcell.NewEventKey(tcell.KeyRune, 'x', tcell.ModNone), func(p tview.Primitive) {})

	if v.scrollOffset != 0 {
		t.Fatalf("expected input to reset scrollback to bottom, got offset %d", v.scrollOffset)
	}
	if len(backend.writes) != 1 || string(backend.writes[0]) != "x" {
		t.Fatalf("expected backend to receive typed rune, got %#v", backend.writes)
	}
}

func TestViewPasteHandlerUsesBracketedPasteWhenEnabled(t *testing.T) {
	v := New(nil)
	backend := &stubBackend{}
	v.backend = backend
	_, _ = v.emu.Write([]byte("\x1b[?2004h"))

	handler := v.PasteHandler()
	handler("hello", func(p tview.Primitive) {})

	if len(backend.writes) != 1 {
		t.Fatalf("expected one backend write, got %d", len(backend.writes))
	}
	if got := string(backend.writes[0]); got != "\x1b[200~hello\x1b[201~" {
		t.Fatalf("expected bracketed paste payload, got %q", got)
	}
}

func TestViewPasteHandlerUsesPlainPasteWhenDisabled(t *testing.T) {
	v := New(nil)
	backend := &stubBackend{}
	v.backend = backend

	handler := v.PasteHandler()
	handler("hello", func(p tview.Primitive) {})

	if len(backend.writes) != 1 {
		t.Fatalf("expected one backend write, got %d", len(backend.writes))
	}
	if got := string(backend.writes[0]); got != "hello" {
		t.Fatalf("expected plain paste payload, got %q", got)
	}
}

func TestDrawStyleSwapsColorsInReverseVideo(t *testing.T) {
	style := tcell.StyleDefault.Foreground(tcell.ColorRed).Background(tcell.ColorBlue).Bold(true)

	got := drawStyle(style, true)
	fg, bg, attr := got.Decompose()
	if fg != tcell.ColorBlue || bg != tcell.ColorRed {
		t.Fatalf("expected swapped colors, got fg=%v bg=%v", fg, bg)
	}
	if attr&tcell.AttrBold == 0 {
		t.Fatalf("expected attributes to be preserved, got %v", attr)
	}
}

func TestViewFocusReportsWhenEnabled(t *testing.T) {
	v := New(nil)
	backend := &stubBackend{}
	v.backend = backend
	_, _ = v.emu.Write([]byte("\x1b[?1004h"))

	v.Focus(nil)
	v.Blur()

	if len(backend.writes) != 2 {
		t.Fatalf("expected two backend writes, got %d", len(backend.writes))
	}
	if got := string(backend.writes[0]); got != "\x1b[I" {
		t.Fatalf("expected focus-in report, got %q", got)
	}
	if got := string(backend.writes[1]); got != "\x1b[O" {
		t.Fatalf("expected focus-out report, got %q", got)
	}
}

func TestViewFocusDoesNotReportWhenDisabled(t *testing.T) {
	v := New(nil)
	backend := &stubBackend{}
	v.backend = backend

	v.Focus(nil)
	v.Blur()

	if len(backend.writes) != 0 {
		t.Fatalf("expected no focus reports when disabled, got %#v", backend.writes)
	}
}

func TestMouseEventToBytesUsesSGREncoding(t *testing.T) {
	ss := Snapshot{MouseVT200: true, MouseSGR: true}
	ev := tcell.NewEventMouse(4, 6, tcell.Button1, tcell.ModCtrl)

	got, ok := mouseEventToBytes(tview.MouseLeftDown, ev, ss, 2, 3)
	if !ok {
		t.Fatalf("expected mouse event to be encoded")
	}
	if string(got) != "\x1b[<16;3;4M" {
		t.Fatalf("expected sgr mouse report, got %q", got)
	}
}

func TestMouseEventToBytesUsesSGRReleaseEncoding(t *testing.T) {
	tests := []struct {
		name   string
		action tview.MouseAction
		want   string
	}{
		{name: "left", action: tview.MouseLeftUp, want: "\x1b[<0;2;2m"},
		{name: "middle", action: tview.MouseMiddleUp, want: "\x1b[<1;2;2m"},
		{name: "right", action: tview.MouseRightUp, want: "\x1b[<2;2;2m"},
	}

	ss := Snapshot{MouseVT200: true, MouseSGR: true}
	ev := tcell.NewEventMouse(1, 1, 0, tcell.ModNone)

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := mouseEventToBytes(tc.action, ev, ss, 0, 0)
			if !ok {
				t.Fatalf("expected mouse event to be encoded")
			}
			if string(got) != tc.want {
				t.Fatalf("expected sgr release %q, got %q", tc.want, got)
			}
		})
	}
}

func TestMouseEventToBytesUsesClassicEncoding(t *testing.T) {
	ss := Snapshot{MouseVT200: true}
	ev := tcell.NewEventMouse(1, 2, tcell.Button1, tcell.ModNone)

	got, ok := mouseEventToBytes(tview.MouseLeftDown, ev, ss, 0, 0)
	if !ok {
		t.Fatalf("expected mouse event to be encoded")
	}
	want := []byte{0x1b, '[', 'M', 32, 34, 35}
	if string(got) != string(want) {
		t.Fatalf("expected classic mouse report %v, got %v", want, got)
	}
}

func TestMouseEventToBytesUsesMotionOnlyWhenConfigured(t *testing.T) {
	ev := tcell.NewEventMouse(2, 2, tcell.Button1, tcell.ModNone)

	if _, ok := mouseEventToBytes(tview.MouseMove, ev, Snapshot{MouseVT200: true}, 0, 0); ok {
		t.Fatalf("expected motion ignored without 1002/1003")
	}
	if _, ok := mouseEventToBytes(tview.MouseMove, ev, Snapshot{MouseButtonEvt: true}, 0, 0); !ok {
		t.Fatalf("expected motion encoded with button-event tracking")
	}
	if _, ok := mouseEventToBytes(tview.MouseMove, tcell.NewEventMouse(2, 2, 0, tcell.ModNone), Snapshot{MouseAnyEvt: true}, 0, 0); !ok {
		t.Fatalf("expected motion encoded with any-event tracking")
	}
}

func TestMouseEventToBytesUsesX10PressOnly(t *testing.T) {
	ss := Snapshot{MouseX10: true}

	if _, ok := mouseEventToBytes(tview.MouseLeftDown, tcell.NewEventMouse(1, 1, tcell.Button1, tcell.ModNone), ss, 0, 0); !ok {
		t.Fatalf("expected x10 press to be encoded")
	}
	if _, ok := mouseEventToBytes(tview.MouseLeftUp, tcell.NewEventMouse(1, 1, 0, tcell.ModNone), ss, 0, 0); ok {
		t.Fatalf("expected x10 release to be ignored")
	}
	if _, ok := mouseEventToBytes(tview.MouseMove, tcell.NewEventMouse(1, 1, tcell.Button1, tcell.ModNone), ss, 0, 0); ok {
		t.Fatalf("expected x10 motion to be ignored")
	}
}

func TestViewSelectedTextUsesDraggedSelection(t *testing.T) {
	v := New(nil)
	v.SetRect(0, 0, 8, 2)
	v.emu.Resize(8, 2)
	_, _ = v.emu.Write([]byte("hello\nworld"))

	if !v.StartSelection(1, 0) {
		t.Fatal("expected selection start to succeed")
	}
	if !v.UpdateSelection(3, 1) {
		t.Fatal("expected selection update to succeed")
	}

	if got := v.SelectedText(); got != "ello\nworl" {
		t.Fatalf("SelectedText() = %q, want %q", got, "ello\nworl")
	}
}

func TestViewClearSelectionResetsState(t *testing.T) {
	v := New(nil)
	v.SetRect(0, 0, 6, 1)
	v.emu.Resize(6, 1)
	_, _ = v.emu.Write([]byte("hello"))

	if !v.StartSelection(0, 0) || !v.UpdateSelection(2, 0) {
		t.Fatal("expected selection setup to succeed")
	}
	if !v.HasSelection() {
		t.Fatal("HasSelection() = false, want true")
	}

	v.ClearSelection()

	if v.HasSelection() {
		t.Fatal("HasSelection() = true after ClearSelection")
	}
	if got := v.SelectedText(); got != "" {
		t.Fatalf("SelectedText() = %q after ClearSelection, want empty", got)
	}
}

func TestViewMouseHandlerReportsWhenEnabled(t *testing.T) {
	v := New(nil)
	v.SetRect(0, 0, 10, 5)
	backend := &stubBackend{}
	v.backend = backend
	_, _ = v.emu.Write([]byte("\x1b[?1000h\x1b[?1006h"))

	handler := v.MouseHandler()
	consumed, _ := handler(tview.MouseLeftDown, tcell.NewEventMouse(1, 1, tcell.Button1, tcell.ModNone), func(p tview.Primitive) {})

	if !consumed {
		t.Fatalf("expected mouse event to be consumed")
	}
	if len(backend.writes) != 1 {
		t.Fatalf("expected one mouse report write, got %d", len(backend.writes))
	}
	if got := string(backend.writes[0]); got != "\x1b[<0;2;2M" {
		t.Fatalf("expected sgr mouse sequence, got %q", got)
	}
}

func TestViewDrawReservesColumnForScrollbar(t *testing.T) {
	v := New(nil)
	v.SetScrollbar(true)
	v.SetRect(0, 0, 6, 4)
	backend := &stubBackend{}
	v.backend = backend

	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatalf("init simulation screen: %v", err)
	}
	defer screen.Fini()

	v.Draw(screen)

	if backend.cols != 5 || backend.rows != 4 {
		t.Fatalf("expected backend resize to terminal area 5x4, got %dx%d", backend.cols, backend.rows)
	}
}

func TestViewDrawRendersScrollbarThumb(t *testing.T) {
	v := New(nil)
	v.SetScrollbar(true)
	v.SetRect(0, 0, 5, 3)
	v.emu = NewEmulator(4, 3)
	_, _ = v.emu.Write([]byte("1111\n2222\n3333\n4444"))
	v.ScrollbackPageUp()

	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatalf("init simulation screen: %v", err)
	}
	defer screen.Fini()

	v.Draw(screen)

	var thumbCells int
	for row := 0; row < 3; row++ {
		mainc, _, _, _ := screen.GetContent(4, row)
		if mainc == '█' {
			thumbCells++
		}
	}
	if thumbCells == 0 {
		t.Fatalf("expected scrollbar thumb to be drawn")
	}
}

func TestViewMouseHandlerUsesScrollbar(t *testing.T) {
	v := New(nil)
	v.SetScrollbar(true)
	v.SetRect(0, 0, 5, 4)
	v.emu = NewEmulator(4, 4)
	_, _ = v.emu.Write([]byte("1111\n2222\n3333\n4444\n5555\n6666\n7777\n8888"))

	handler := v.MouseHandler()
	consumed, _ := handler(tview.MouseLeftDown, tcell.NewEventMouse(4, 0, tcell.Button1, tcell.ModNone), func(p tview.Primitive) {})

	if !consumed {
		t.Fatalf("expected scrollbar click to be consumed")
	}
	if v.scrollOffset == 0 {
		t.Fatalf("expected scrollbar click to move scroll offset")
	}
}

func TestViewSyncTitleCallsHandler(t *testing.T) {
	v := New(nil)
	var got string
	v.SetTitleHandler(func(_ *View, title string) {
		got = title
	})

	_, _ = v.emu.Write([]byte("\x1b]2;remote shell\x07"))
	v.syncTitle()

	if got != "remote shell" {
		t.Fatalf("expected title handler to receive remote title, got %q", got)
	}
}

func TestViewPTYAccumulatesScrollback(t *testing.T) {
	backend, err := NewPTYBackend(exec.Command("/bin/sh", "-lc", "seq 1 200"), 20, 5)
	if err != nil {
		t.Fatalf("new pty backend: %v", err)
	}
	defer backend.Close()

	v := New(nil)
	v.Attach(backend)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		_, rows := v.ScrollbackStatus()
		if rows > 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	_, rows := v.ScrollbackStatus()
	t.Fatalf("expected PTY-backed view to accumulate scrollback, got rows=%d", rows)
}

func TestViewInteractiveShellAccumulatesScrollback(t *testing.T) {
	backend, err := NewPTYBackend(exec.Command("/bin/sh"), 20, 5)
	if err != nil {
		t.Fatalf("new pty backend: %v", err)
	}
	defer backend.Close()

	v := New(nil)
	v.Attach(backend)

	if err := writeAll(backend, []byte("seq 1 200\r")); err != nil {
		t.Fatalf("write command: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		_, rows := v.ScrollbackStatus()
		if rows > 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	_, rows := v.ScrollbackStatus()
	t.Fatalf("expected interactive shell view to accumulate scrollback, got rows=%d", rows)
}

func TestViewUserShellAccumulatesScrollback(t *testing.T) {
	shell := os.Getenv("SHELL")
	if shell == "" {
		t.Skip("SHELL is not set")
	}

	backend, err := NewPTYBackend(exec.Command(shell), 20, 5)
	if err != nil {
		t.Fatalf("new pty backend: %v", err)
	}
	defer backend.Close()

	v := New(nil)
	v.Attach(backend)

	if err := writeAll(backend, []byte("seq 1 200\r")); err != nil {
		t.Fatalf("write command: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		_, rows := v.ScrollbackStatus()
		if rows > 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	_, rows := v.ScrollbackStatus()
	t.Fatalf("expected user shell view to accumulate scrollback, got rows=%d", rows)
}

func TestViewUserShellWithXtermEnvAccumulatesScrollback(t *testing.T) {
	shell := os.Getenv("SHELL")
	if shell == "" {
		t.Skip("SHELL is not set")
	}

	cmd := exec.Command(shell)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	backend, err := NewPTYBackend(cmd, 20, 5)
	if err != nil {
		t.Fatalf("new pty backend: %v", err)
	}
	defer backend.Close()

	v := New(nil)
	v.Attach(backend)

	if err := writeAll(backend, []byte("seq 1 200\r")); err != nil {
		t.Fatalf("write command: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		_, rows := v.ScrollbackStatus()
		if rows > 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	_, rows := v.ScrollbackStatus()
	t.Fatalf("expected xterm-env shell view to accumulate scrollback, got rows=%d", rows)
}

// TestSelectionDragPathAllocationsFree pins the performance contract that made
// mouse-drag selection laggy: hit-testing one pointer position must not
// allocate. The pre-fix path copied the visible grid AND the entire
// scrollback on every mouse-move event (Snapshot + allRows inside
// selectionCellFromScreen), so drag latency scaled with scrollback size.
func TestSelectionDragPathAllocationsFree(t *testing.T) {
	v := New(nil)
	v.SetRect(0, 0, 12, 5)

	// Fill past the visible window so the emulator holds scrollback rows —
	// the condition that made the old per-event copy expensive.
	var payload []byte
	for range 200 {
		payload = append(payload, []byte("scrollback line\n")...)
	}
	if _, err := v.emu.Write(payload); err != nil {
		t.Fatalf("feed emulator: %v", err)
	}

	allocs := testing.AllocsPerRun(500, func() {
		if _, ok := v.selectionCellFromScreen(5, 2); !ok {
			t.Fatal("hit-test failed inside rect")
		}
	})
	if allocs != 0 {
		t.Fatalf("selectionCellFromScreen allocated %v times per call, want 0", allocs)
	}

	// Same-cell move must not dirty the selection (no redraw) — cheap check
	// that UpdateSelection's no-op fast path engages.
	if !v.StartSelection(5, 2) {
		t.Fatal("start selection failed")
	}
	before := v.selection.current
	if !v.UpdateSelection(5, 2) {
		t.Fatal("update selection failed")
	}
	if v.selection.current != before {
		t.Fatalf("same-cell move mutated selection: %+v -> %+v", before, v.selection.current)
	}
}

// TestSnapshotAtCopiesOnlyVisibleWindow pins the Draw-side contract: building
// a frame snapshot must not copy rows outside the visible window, regardless
// of how much scrollback the emulator holds.
func TestSnapshotAtCopiesOnlyVisibleWindow(t *testing.T) {
	e := NewEmulator(80, 24)
	var payload []byte
	for i := range 900 {
		payload = append(payload, []byte(strconv.Itoa(i%10)+": scrollback row padding padding padding\n")...)
	}
	if _, err := e.Write(payload); err != nil {
		t.Fatalf("feed emulator: %v", err)
	}
	_, _, scrollbackRows := e.Dimensions()
	if scrollbackRows < 500 {
		t.Fatalf("precondition: expected large scrollback, got %d rows", scrollbackRows)
	}

	ss := e.SnapshotAt(0)
	if ss.Rows != 24 || ss.Cols != 80 {
		t.Fatalf("snapshot geometry = %dx%d, want 80x24", ss.Cols, ss.Rows)
	}
	if len(ss.Cells) != ss.Rows {
		t.Fatalf("snapshot holds %d row slices, want visible %d", len(ss.Cells), ss.Rows)
	}
	for y := range ss.Cells {
		if len(ss.Cells[y]) != ss.Cols {
			t.Fatalf("snapshot row %d holds %d cells, want %d", y, len(ss.Cells[y]), ss.Cols)
		}
	}

	// Allocation budget: one outer slice + one inner slice per visible row.
	// The old implementation allocated one slice per scrollback row (~900+).
	allocs := testing.AllocsPerRun(200, func() {
		_ = e.SnapshotAt(0)
	})
	if allocs > float64(ss.Rows)+2 {
		t.Fatalf("SnapshotAt allocated %v times, want <= %d (visible rows + overhead)", allocs, ss.Rows+2)
	}
}

// BenchmarkSelectionCellFromScreen measures the per-mouse-move hit-test cost
// with a full scrollback — the hot path of drag selection.
func BenchmarkSelectionCellFromScreen(b *testing.B) {
	v := New(nil)
	v.SetRect(0, 0, 82, 26)
	var payload []byte
	for range 1000 {
		payload = append(payload, []byte("benchmark scrollback line with some content\n")...)
	}
	if _, err := v.emu.Write(payload); err != nil {
		b.Fatalf("feed emulator: %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = v.selectionCellFromScreen(40, 12)
	}
}

// BenchmarkSnapshotAt measures the per-frame snapshot cost with a full
// scrollback — the hot path of every Draw.
func BenchmarkSnapshotAt(b *testing.B) {
	e := NewEmulator(80, 24)
	var payload []byte
	for i := range 1000 {
		payload = append(payload, []byte(strconv.Itoa(i%10)+": benchmark scrollback row content here\n")...)
	}
	if _, err := e.Write(payload); err != nil {
		b.Fatalf("feed emulator: %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = e.SnapshotAt(0)
	}
}
