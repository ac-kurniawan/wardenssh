package tvxterm

import (
	"strconv"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
)

// maxScrollbackForTest mirrors the emulator's cap. Kept local so the test
// states the number it depends on rather than importing the field.
const maxScrollbackForTest = 1000

// anchorView builds a view with `rows` visible rows and `lines` lines of
// scrollback. Line N reads "line-N", so a test can tell which row is visible.
// It draws once first, because Draw is what sizes the emulator to the view's
// rect (the emulator starts at its default 80x24).
func anchorView(t *testing.T, rows, lines int) (*View, *pipeBackend) {
	t.Helper()
	v := New(nil)
	v.SetRect(0, 0, 20, rows)
	backend := &pipeBackend{}
	v.Attach(backend)
	draw(v, rows)
	payload := make([]byte, 0, lines*8)
	for i := range lines {
		payload = append(payload, []byte("line-"+strconv.Itoa(i)+"\n")...)
	}
	if !v.Feed(payload) {
		t.Fatal("precondition: Feed failed")
	}
	return v, backend
}

// draw renders the view onto a simulation screen sized to the view's rect,
// which is what applies SetRect to the emulator.
func draw(v *View, rows int) {
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		panic(err)
	}
	defer screen.Fini()
	screen.SetSize(20, rows)
	v.Draw(screen)
}

// scrollOffset reads the view's current scroll offset under its lock.
func scrollOffset(v *View) int {
	offset, _ := v.ScrollbackStatus()
	return offset
}

// firstVisibleRow returns the absolute index of the row shown at the top of the
// view, computed from the same geometry SnapshotAt uses.
func firstVisibleRow(v *View) int {
	_, viewRows, scrollbackRows := v.emu.Dimensions()
	total := scrollbackRows + viewRows
	return total - viewRows - scrollOffset(v)
}

// topRowText returns the text of the row currently drawn at the top of the view.
func topRowText(v *View) string {
	return rowString(v.emu.SnapshotAt(scrollOffset(v)).Cells[0])
}

// TestScrollAnchorKeepsContentUnderNewOutput: while scrolled up, a new line of
// output must not move the content under the reader. The scroll offset is
// measured from the bottom, so appending a row without adjusting the offset
// slides the whole window up by one line — the defect this pins.
func TestScrollAnchorKeepsContentUnderNewOutput(t *testing.T) {
	v, _ := anchorView(t, 4, 20)
	defer v.Close()

	v.ScrollbackUp(5)
	before := firstVisibleRow(v)
	text := topRowText(v)

	if !v.Feed([]byte("fresh-line\n")) {
		t.Fatal("Feed failed")
	}

	if after := firstVisibleRow(v); after != before {
		t.Fatalf("new output moved the anchored view: first visible row %d -> %d", before, after)
	}
	if got := topRowText(v); got != text {
		t.Fatalf("new output changed the anchored content: %q -> %q", text, got)
	}
}

// TestScrollAnchorIgnoresOutputAtBottom: at the bottom of the buffer the view
// must keep following the live output, not anchor itself.
func TestScrollAnchorIgnoresOutputAtBottom(t *testing.T) {
	v, _ := anchorView(t, 4, 20)
	defer v.Close()

	if offset := scrollOffset(v); offset != 0 {
		t.Fatalf("precondition: expected to start at the bottom, offset=%d", offset)
	}
	v.Feed([]byte("fresh-line\n"))
	if offset := scrollOffset(v); offset != 0 {
		t.Fatalf("expected the live view to stay pinned to the bottom, offset=%d", offset)
	}
}

// TestScrollAnchorPinsToOldestLineWhenTrimmed: once the scrollback limit is
// reached, old rows are dropped from the front. A view anchored to content that
// no longer exists must come to rest on the oldest retained row instead of
// jumping.
func TestScrollAnchorPinsToOldestLineWhenTrimmed(t *testing.T) {
	v, _ := anchorView(t, 4, maxScrollbackForTest)
	defer v.Close()

	v.ScrollbackTop()
	before := scrollOffset(v)

	// Push enough output to force the cap to trim several rows. The view was on
	// the oldest line, whose content is then dropped; the offset cannot grow past
	// the cap, so the view must stay pinned to the oldest retained line instead
	// of sliding to the bottom.
	for range 10 {
		v.Feed([]byte("extra-line\n"))
	}

	after, rows := v.ScrollbackStatus()
	if rows != maxScrollbackForTest {
		t.Fatalf("expected the scrollback to stay at its cap, got %d rows", rows)
	}
	if after != rows {
		t.Fatalf("expected the view to stay at the oldest retained line, offset=%d rows=%d (was %d)", after, rows, before)
	}
	if start := firstVisibleRow(v); start != 0 {
		t.Fatalf("expected the trimmed view to rest on the oldest row, first visible row=%d", start)
	}
	if got := topRowText(v); !strings.HasPrefix(got, "line-") {
		t.Fatalf("expected real content at the top of the trimmed scrollback, got %q", got)
	}
}

// TestScrollPageIsOneLineLessThanTheScreen: xterm's Shift+PgUp/PgDn move by a
// page minus one line so a line of context survives the jump.
func TestScrollPageIsOneLineLessThanTheScreen(t *testing.T) {
	v, _ := anchorView(t, 4, 40)
	defer v.Close()

	v.ScrollbackPageUp()
	if offset := scrollOffset(v); offset != 3 {
		t.Fatalf("expected page-up to move 3 lines on a 4-row screen, got %d", offset)
	}
	v.ScrollbackPageDown()
	if offset := scrollOffset(v); offset != 0 {
		t.Fatalf("expected page-down to return to the bottom, offset=%d", offset)
	}
}

// TestScrollbackTopAndBottom: Shift+Home jumps to the oldest retained line,
// Shift+End back to the live output.
func TestScrollbackTopAndBottom(t *testing.T) {
	v, _ := anchorView(t, 4, 40)
	defer v.Close()

	v.ScrollbackTop()
	offset, rows := v.ScrollbackStatus()
	if offset != rows {
		t.Fatalf("expected top to sit at the oldest line, offset=%d rows=%d", offset, rows)
	}
	v.ScrollbackBottom()
	if offset := scrollOffset(v); offset != 0 {
		t.Fatalf("expected bottom to return to the live screen, offset=%d", offset)
	}
}

// TestSendKeyShiftPageScrollsLocally: Shift+PgUp/PgDn and Shift+Home/End move
// the pane's own history and must not reach the remote app.
func TestSendKeyShiftPageScrollsLocally(t *testing.T) {
	v, backend := anchorView(t, 4, 40)
	defer v.Close()

	v.SendKey(tcell.NewEventKey(tcell.KeyPgUp, 0, tcell.ModShift))
	if offset := scrollOffset(v); offset != 3 {
		t.Fatalf("expected Shift+PgUp to scroll one page minus a line, offset=%d", offset)
	}
	v.SendKey(tcell.NewEventKey(tcell.KeyPgDn, 0, tcell.ModShift))
	if offset := scrollOffset(v); offset != 0 {
		t.Fatalf("expected Shift+PgDn to return to the bottom, offset=%d", offset)
	}
	v.SendKey(tcell.NewEventKey(tcell.KeyHome, 0, tcell.ModShift))
	if offset := scrollOffset(v); offset == 0 {
		t.Fatal("expected Shift+Home to jump to the oldest line")
	}
	v.SendKey(tcell.NewEventKey(tcell.KeyEnd, 0, tcell.ModShift))
	if offset := scrollOffset(v); offset != 0 {
		t.Fatalf("expected Shift+End to return to the bottom, offset=%d", offset)
	}
	if got := len(backend.writes); got != 0 {
		t.Fatalf("scroll keys must not be sent to the remote, got %d writes: %q", got, backend.writes)
	}
}

// TestSendKeyPlainPageUpReachesRemote: without Shift, PgUp is the remote app's
// key, not the pane's.
func TestSendKeyPlainPageUpReachesRemote(t *testing.T) {
	v, backend := anchorView(t, 4, 40)
	defer v.Close()

	v.SendKey(tcell.NewEventKey(tcell.KeyPgUp, 0, tcell.ModNone))
	if len(backend.writes) != 1 {
		t.Fatalf("expected plain PgUp to be forwarded to the remote, got %d writes", len(backend.writes))
	}
	if got := string(backend.writes[0]); got != "\x1b[5~" {
		t.Fatalf("plain PgUp sent %q, want %q", got, "\x1b[5~")
	}
}

// TestSendKeyResetsScroll: a key that reaches the remote means the user is
// typing again, so the view returns to the live output.
func TestSendKeyResetsScroll(t *testing.T) {
	v, _ := anchorView(t, 4, 40)
	defer v.Close()

	v.ScrollbackPageUp()
	if offset := scrollOffset(v); offset == 0 {
		t.Fatal("precondition: expected a scrolled view")
	}
	v.SendKey(tcell.NewEventKey(tcell.KeyPgUp, 0, tcell.ModNone))
	if offset := scrollOffset(v); offset != 0 {
		t.Fatalf("expected typing to snap the view back to the bottom, offset=%d", offset)
	}
}

// TestViewReportsRemoteTerminalModes: the pane routes the wheel by asking
// whether the app owns the mouse and whether it is on the alternate screen.
func TestViewReportsRemoteTerminalModes(t *testing.T) {
	v, _ := anchorView(t, 4, 8)
	defer v.Close()

	if v.RemoteMouseReporting() {
		t.Fatal("expected mouse reporting to start disabled")
	}
	if v.UsingAltScreen() {
		t.Fatal("expected the primary screen at start")
	}
	if !v.Feed([]byte("\x1b[?1000h\x1b[?1006h\x1b[?1049h")) {
		t.Fatal("Feed failed")
	}
	if !v.RemoteMouseReporting() {
		t.Fatal("expected the view to report the app's mouse mode")
	}
	if !v.UsingAltScreen() {
		t.Fatal("expected the view to report the alternate screen")
	}
}

// TestScrollbackUpFromTopStaysPut: scrolling up at the oldest line is a no-op.
func TestScrollbackUpFromTopStaysPut(t *testing.T) {
	v, _ := anchorView(t, 4, 12)
	defer v.Close()

	v.ScrollbackTop()
	top := scrollOffset(v)
	v.ScrollbackUp(5)
	if offset := scrollOffset(v); offset != top {
		t.Fatalf("expected scrolling up at the top to stay at %d, got %d", top, offset)
	}
}
