package tvxterm

import (
	"fmt"
	"io"
	"math"
	"os"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// View is a tview primitive that renders a terminal stream inside a boxed widget.
// Design intent:
//   - frontend: tview primitive
//   - state: tiny VT-like emulator
//   - backend: PTY/SSH/anything implementing Backend
//
// This mirrors the separation used by gowid's terminal widget: input/output are
// not tied to a subprocess launcher, and drawing happens from an intermediate
// terminal state rather than directly from raw bytes.
type View struct {
	*tview.Box

	app *tview.Application

	// emu is the terminal state. Every accessor on it takes its own lock and
	// hands back a private copy, so no view-level lock is needed — Draw holds
	// the emulator read lock only for the length of a snapshot, never across
	// screen rendering, so a slow frame cannot stall the backend read loop.
	emu *Emulator

	mu            sync.RWMutex // guards backend/debug/handlers/closed/scrollOffset/scrollbar/lastTitle/lastBackend*
	backend       Backend
	debug         *os.File
	onBackendExit func(*View, error)
	onTitle       func(*View, string)
	focused       bool
	closed        bool
	scrollOffset  int
	scrollbar     bool
	lastTitle       string
	lastBackendCols int
	lastBackendRows int

	// selectionMu guards the selection range. It is separate from mu so the
	// Draw hot path can read the selection once per frame instead of taking
	// an RWMutex per rendered cell.
	selectionMu sync.Mutex
	selection   selectionState

	// redraw coalescing: at most one redrawLoop goroutine per view, and it
	// collapses bursty requestRedraw calls (PTY output, selection drags,
	// scroll) into a single QueueUpdateDraw per burst.
	redrawMu      sync.Mutex
	redrawPending bool
}

type selectionCell struct {
	row int
	col int
}

type selectionState struct {
	active  bool
	anchor  selectionCell
	current selectionCell
}

// New creates a terminal view bound to the given tview application.
//
// The application reference is used for redraw scheduling and optional
// focus/paste integration. Passing nil is allowed for tests or headless use.
func New(app *tview.Application) *View {
	var debug *os.File
	if path := os.Getenv("TVIEW_TERM_DEBUG_IO"); path != "" {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err == nil {
			debug = f
		}
	}
	return &View{
		Box:   tview.NewBox(),
		app:   app,
		emu:   NewEmulator(80, 24),
		debug: debug,
	}
}

// Attach connects a backend to the view and starts reading from it.
//
// Callers typically create one backend per view and attach it once.
func (v *View) Attach(backend Backend) {
	v.mu.Lock()
	v.backend = backend
	v.mu.Unlock()

	go v.readLoop(backend)
}

// SetBackendExitHandler installs a callback invoked when the attached backend
// read loop exits with an error or EOF.
func (v *View) SetBackendExitHandler(fn func(*View, error)) *View {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.onBackendExit = fn
	return v
}

// SetTitleHandler installs a callback for OSC title updates observed from the
// terminal stream.
func (v *View) SetTitleHandler(fn func(*View, string)) *View {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.onTitle = fn
	return v
}

// SetScrollbar enables or disables the built-in scrollback scrollbar.
//
// When enabled, the rightmost column of the view is reserved for the bar and
// the terminal content is rendered one column narrower.
func (v *View) SetScrollbar(enabled bool) *View {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.scrollbar = enabled
	return v
}

// Close closes the current backend and releases debug-log resources.
func (v *View) Close() error {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.closed = true
	if v.debug != nil {
		_ = v.debug.Close()
		v.debug = nil
	}
	if v.backend != nil {
		return v.backend.Close()
	}
	return nil
}

func (v *View) Draw(screen tcell.Screen) {
	v.Box.DrawForSubclass(screen, v)

	x, y, width, height := v.GetInnerRect()
	if width <= 0 || height <= 0 {
		return
	}

	contentWidth, scrollbarX, showScrollbar := v.layout(width)
	if contentWidth <= 0 {
		if showScrollbar {
			v.drawScrollbar(screen, x+scrollbarX, y, height)
		}
		return
	}

	// SnapshotAt hands back a private copy of just the visible window; the
	// emulator lock is held only for that copy, so the SetContent loop below
	// (which can be slow on large frames) never stalls the backend read loop.
	v.emu.Resize(contentWidth, height)
	v.resizeBackend(contentWidth, height)

	v.mu.RLock()
	scrollOffset := v.scrollOffset
	v.mu.RUnlock()
	ss := v.emu.SnapshotAt(scrollOffset)
	viewStart := v.visibleRowStart(ss)

	// Read the selection range once for the whole frame instead of taking a
	// lock per cell (previously one RWMutex acquire/release per SetContent).
	v.selectionMu.Lock()
	sel := v.selection
	v.selectionMu.Unlock()
	selStart, selEnd, selActive := orderedSelectionRange(sel)

	for row := range ss.Rows {
		absRow := viewStart + row
		// Fast path: row entirely outside the selection — no per-cell test.
		rowSel := selActive && absRow >= selStart.row && absRow <= selEnd.row
		for col := range ss.Cols {
			cell := ss.Cells[row][col]
			if cell.Occupied && cell.Width == 0 {
				continue
			}
			style := drawStyle(cell.Style, ss.ReverseVideo)
			if rowSel && selectionRangeContains(selStart, selEnd, absRow, col) {
				style = selectionStyle(style)
			}
			screen.SetContent(x+col, y+row, cell.Ch, cell.Comb, style)
		}
	}

	if v.focused && ss.CursorVis {
		screen.ShowCursor(x+ss.CursorX, y+ss.CursorY)
	}

	if showScrollbar {
		v.drawScrollbar(screen, x+scrollbarX, y, height)
	}
}

func (v *View) InputHandler() func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
	return v.WrapInputHandler(func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
		v.mu.RLock()
		backend := v.backend
		v.mu.RUnlock()
		if backend == nil {
			return
		}

		ss := v.emu.Snapshot()
		seq, ok := keyToBytes(event, ss.AppCursor || ss.AppKeypad)
		if !ok {
			return
		}
		v.sendInput(backend, seq)
	})
}

func (v *View) PasteHandler() func(text string, setFocus func(p tview.Primitive)) {
	return v.WrapPasteHandler(func(text string, setFocus func(p tview.Primitive)) {
		v.mu.RLock()
		backend := v.backend
		v.mu.RUnlock()
		if backend == nil || text == "" {
			return
		}

		ss := v.emu.Snapshot()
		payload := []byte(text)
		if ss.BracketedPaste {
			payload = append([]byte("\x1b[200~"), payload...)
			payload = append(payload, []byte("\x1b[201~")...)
		}
		v.sendInput(backend, payload)
	})
}

// SendKey writes a single key event to the attached backend using the same
// encoding rules as interactive input handling.
func (v *View) SendKey(event *tcell.EventKey) bool {
	if event == nil {
		return false
	}

	v.mu.RLock()
	backend := v.backend
	v.mu.RUnlock()
	if backend == nil {
		return false
	}

	ss := v.emu.Snapshot()
	seq, ok := keyToBytes(event, ss.AppCursor || ss.AppKeypad)
	if !ok {
		return false
	}
	v.sendInput(backend, seq)
	return true
}

// SendPaste writes pasted text to the attached backend, preserving bracketed
// paste behavior when enabled by the remote terminal.
func (v *View) SendPaste(text string) bool {
	if text == "" {
		return false
	}

	v.mu.RLock()
	backend := v.backend
	v.mu.RUnlock()
	if backend == nil {
		return false
	}

	ss := v.emu.Snapshot()
	payload := []byte(text)
	if ss.BracketedPaste {
		payload = append([]byte("\x1b[200~"), payload...)
		payload = append(payload, []byte("\x1b[201~")...)
	}
	v.sendInput(backend, payload)
	return true
}

// StartSelection begins a local text selection using screen coordinates.
func (v *View) StartSelection(screenX, screenY int) bool {
	cell, ok := v.selectionCellFromScreen(screenX, screenY)
	if !ok {
		return false
	}

	v.selectionMu.Lock()
	v.selection.active = true
	v.selection.anchor = cell
	v.selection.current = cell
	v.selectionMu.Unlock()
	v.requestRedraw()
	return true
}

// UpdateSelection adjusts the active selection using screen coordinates.
func (v *View) UpdateSelection(screenX, screenY int) bool {
	cell, ok := v.selectionCellFromScreen(screenX, screenY)
	if !ok {
		return false
	}

	v.selectionMu.Lock()
	if !v.selection.active {
		v.selection.active = true
		v.selection.anchor = cell
	}
	if v.selection.current == cell {
		// Pointer stayed on the same cell: nothing to repaint. Dragging
		// across a terminal emits a mouse event per pixel, so most events
		// land on the cell already selected — skip the redraw entirely.
		v.selectionMu.Unlock()
		return true
	}
	v.selection.current = cell
	v.selectionMu.Unlock()
	v.requestRedraw()
	return true
}

// ClearSelection removes the current local text selection.
func (v *View) ClearSelection() {
	v.selectionMu.Lock()
	v.selection = selectionState{}
	v.selectionMu.Unlock()
	v.requestRedraw()
}

// HasSelection reports whether a non-empty local text selection exists.
func (v *View) HasSelection() bool {
	v.selectionMu.Lock()
	defer v.selectionMu.Unlock()
	if !v.selection.active {
		return false
	}
	return v.selection.anchor != v.selection.current
}

// SelectedText returns the currently selected terminal text.
func (v *View) SelectedText() string {
	v.selectionMu.Lock()
	sel := v.selection
	v.selectionMu.Unlock()
	if !sel.active {
		return ""
	}

	start, end, ok := orderedSelectionRange(sel)
	if !ok {
		return ""
	}
	// Normalize wide-rune columns and clamp rows via the emulator (no copies);
	// then copy only the rows inside the selection — extraction cost scales
	// with selection size, not the whole scrollback.
	var total int
	start.col, total, _ = v.emu.NormalizeCol(start.row, start.col)
	end.col, _, _ = v.emu.NormalizeCol(end.row, end.col)
	if total == 0 {
		return ""
	}
	start.row = clamp(start.row, 0, total-1)
	end.row = clamp(end.row, 0, total-1)
	rows := v.emu.RowsInRange(start.row, end.row)
	if len(rows) == 0 {
		return ""
	}

	var out strings.Builder
	for rowIndex := start.row; rowIndex <= end.row; rowIndex++ {
		row := rows[rowIndex-start.row]
		if len(row) == 0 {
			if rowIndex < end.row {
				out.WriteByte('\n')
			}
			continue
		}

		from := 0
		to := len(row) - 1
		if rowIndex == start.row {
			from = clamp(start.col, 0, len(row)-1)
		}
		if rowIndex == end.row {
			to = clamp(end.col, 0, len(row)-1)
		}
		if from > to {
			from, to = to, from
		}

		line := strings.TrimRight(cellsToString(row[from:to+1]), " ")
		out.WriteString(line)
		if rowIndex < end.row {
			out.WriteByte('\n')
		}
	}
	return out.String()
}

func (v *View) Focus(delegate func(p tview.Primitive)) {
	v.focused = true
	v.reportFocus(true)
}

func (v *View) Blur() {
	v.focused = false
	v.reportFocus(false)
}

func (v *View) HasFocus() bool {
	return v.focused
}

func (v *View) MouseHandler() func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(p tview.Primitive)) (consumed bool, capture tview.Primitive) {
	return v.WrapMouseHandler(func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(p tview.Primitive)) (consumed bool, capture tview.Primitive) {
		if event == nil {
			return false, nil
		}
		x, y := event.Position()
		if !v.InRect(x, y) {
			return false, nil
		}
		if v.handleScrollbarMouse(action, event, setFocus) {
			return true, nil
		}
		v.sendMouse(action, event)
		if action == tview.MouseLeftDown {
			setFocus(v)
			return true, nil
		}
		return true, nil
	})
}

func (v *View) readLoop(backend Backend) {
	buf := make([]byte, 32*1024)
	for {
		n, err := backend.Read(buf)
		if n > 0 {
			v.logBytes("output", buf[:n])
			_, _ = v.emu.Write(buf[:n])
			v.syncTitle()
			for _, resp := range v.emu.DrainResponses() {
				v.logBytes("input", resp)
				_ = writeAll(backend, resp)
			}
			v.requestRedraw()
		}
		if err != nil {
			v.mu.RLock()
			closed := v.closed
			handler := v.onBackendExit
			v.mu.RUnlock()
			if !closed && handler != nil {
				handler(v, err)
				return
			}
			if !closed && v.app != nil {
				v.app.QueueUpdate(func() {
					v.mu.Lock()
					v.closed = true
					v.mu.Unlock()
					v.app.Stop()
				})
			}
			return
		}
		v.mu.RLock()
		closed := v.closed
		v.mu.RUnlock()
		if closed {
			return
		}
	}
}

func (v *View) scrollBy(delta int) {
	_, _, scrollbackRows := v.emu.Dimensions()
	v.mu.Lock()
	v.scrollOffset = clamp(v.scrollOffset+delta, 0, scrollbackRows)
	v.mu.Unlock()
	v.requestRedraw()
}

// ScrollbackUp moves the local scrollback view upward by the given number of
// lines. Non-positive values are treated as 1.
func (v *View) ScrollbackUp(lines int) {
	if lines <= 0 {
		lines = 1
	}
	v.scrollBy(lines)
}

// ScrollbackDown moves the local scrollback view downward by the given number
// of lines. Non-positive values are treated as 1.
func (v *View) ScrollbackDown(lines int) {
	if lines <= 0 {
		lines = 1
	}
	v.scrollBy(-lines)
}

// ScrollbackPageUp moves the local scrollback view by one visible page.
func (v *View) ScrollbackPageUp() {
	_, rows, _ := v.emu.Dimensions()
	v.scrollBy(rows)
}

// ScrollbackPageDown moves the local scrollback view down by one visible page.
func (v *View) ScrollbackPageDown() {
	_, rows, _ := v.emu.Dimensions()
	v.scrollBy(-rows)
}

// ScrollbackStatus returns the current local scroll offset and the total number
// of available scrollback rows.
func (v *View) ScrollbackStatus() (offset int, rows int) {
	v.mu.RLock()
	offset = v.scrollOffset
	v.mu.RUnlock()
	_, _, rows = v.emu.Dimensions()
	return offset, rows
}

func (v *View) scrollToTop() {
	_, _, scrollbackRows := v.emu.Dimensions()
	v.mu.Lock()
	v.scrollOffset = scrollbackRows
	v.mu.Unlock()
	v.requestRedraw()
}

func (v *View) scrollToBottom() {
	v.mu.Lock()
	v.scrollOffset = 0
	v.mu.Unlock()
	v.requestRedraw()
}

func (v *View) resetScrollback() {
	v.mu.Lock()
	v.scrollOffset = 0
	v.mu.Unlock()
}

// requestRedraw schedules a screen redraw on the tview event loop. Calls are
// coalesced: while a redraw is queued, further requests (PTY output bursts,
// per-cell selection drags, wheel scrolls) collapse into that one frame
// instead of each spawning a goroutine that blocks on QueueUpdateDraw.
func (v *View) requestRedraw() {
	if v.app == nil {
		return
	}
	v.redrawMu.Lock()
	if v.redrawPending {
		v.redrawMu.Unlock()
		return
	}
	v.redrawPending = true
	v.redrawMu.Unlock()

	go func() {
		v.app.QueueUpdateDraw(func() {})
		v.redrawMu.Lock()
		v.redrawPending = false
		v.redrawMu.Unlock()
	}()
}

func (v *View) layout(width int) (contentWidth int, scrollbarX int, showScrollbar bool) {
	v.mu.RLock()
	showScrollbar = v.scrollbar && width >= 2
	v.mu.RUnlock()
	if showScrollbar {
		return width - 1, width - 1, true
	}
	return width, 0, false
}

func (v *View) drawScrollbar(screen tcell.Screen, x, y, height int) {
	offset, rows := v.ScrollbackStatus()
	thumbStart, thumbHeight := scrollbarThumb(height, rows, offset)
	trackStyle := tcell.StyleDefault.Foreground(tcell.ColorGray).Background(tcell.ColorBlack)
	thumbStyle := tcell.StyleDefault.Foreground(tcell.ColorWhite).Background(tcell.ColorGray)

	for row := 0; row < height; row++ {
		ch := '│'
		style := trackStyle
		if row >= thumbStart && row < thumbStart+thumbHeight {
			ch = '█'
			style = thumbStyle
		}
		screen.SetContent(x, y+row, ch, nil, style)
	}
}

// selectionRangeContains reports whether absolute (row, col) falls inside the
// ordered selection range. Pure function — callers read the range once per
// frame, so the per-cell test is branch-only.
func selectionRangeContains(start, end selectionCell, row, col int) bool {
	if row < start.row || row > end.row {
		return false
	}
	if start.row == end.row {
		return col >= start.col && col <= end.col
	}
	if row == start.row {
		return col >= start.col
	}
	if row == end.row {
		return col <= end.col
	}
	return true
}

// selectionContains reports whether (row, col) is inside the active selection.
// Retained for API compatibility; Draw uses selectionRangeContains instead.
func (v *View) selectionContains(row, col int) bool {
	v.selectionMu.Lock()
	sel := v.selection
	v.selectionMu.Unlock()
	if !sel.active {
		return false
	}
	start, end, ok := orderedSelectionRange(sel)
	if !ok {
		return false
	}
	return selectionRangeContains(start, end, row, col)
}

func (v *View) visibleRowStart(ss Snapshot) int {
	v.mu.RLock()
	scrollOffset := v.scrollOffset
	v.mu.RUnlock()
	return max(0, ss.ScrollbackRows-scrollOffset)
}

func (v *View) selectionCellFromScreen(screenX, screenY int) (selectionCell, bool) {
	x, y, width, height := v.GetInnerRect()
	if width <= 0 || height <= 0 {
		return selectionCell{}, false
	}

	contentWidth, _, _ := v.layout(width)
	if contentWidth <= 0 {
		return selectionCell{}, false
	}
	if screenX < x || screenX >= x+contentWidth || screenY < y || screenY >= y+height {
		return selectionCell{}, false
	}

	// Geometry only: Dimensions + NormalizeCol read a handful of ints and one
	// row under RLock — no full-grid or full-scrollback copies. This is the
	// per-mouse-move path, so allocations here directly stall drags.
	_, viewRows, scrollbackRows := v.emu.Dimensions()
	localCol := clamp(screenX-x, 0, max(0, contentWidth-1))
	localRow := clamp(screenY-y, 0, max(0, viewRows-1))

	v.mu.RLock()
	scrollOffset := v.scrollOffset
	v.mu.RUnlock()
	absRow := max(0, scrollbackRows-scrollOffset) + localRow

	normCol, totalRows, ok := v.emu.NormalizeCol(absRow, localCol)
	if !ok {
		return selectionCell{}, false
	}
	absRow = clamp(absRow, 0, totalRows-1)
	return selectionCell{row: absRow, col: normCol}, true
}

func orderedSelectionRange(sel selectionState) (selectionCell, selectionCell, bool) {
	if !sel.active {
		return selectionCell{}, selectionCell{}, false
	}
	start := sel.anchor
	end := sel.current
	if start.row > end.row || (start.row == end.row && start.col > end.col) {
		start, end = end, start
	}
	return start, end, true
}

func normalizeSelectionRange(sel selectionState, rows [][]Cell) (selectionCell, selectionCell, bool) {
	start, end, ok := orderedSelectionRange(sel)
	if !ok || len(rows) == 0 {
		return selectionCell{}, selectionCell{}, false
	}

	start.row = clamp(start.row, 0, len(rows)-1)
	end.row = clamp(end.row, 0, len(rows)-1)
	start.col = normalizeSelectionColumn(rows[start.row], start.col)
	end.col = normalizeSelectionColumn(rows[end.row], end.col)
	return start, end, true
}

func normalizeSelectionColumn(row []Cell, col int) int {
	if len(row) == 0 {
		return 0
	}
	col = clamp(col, 0, len(row)-1)
	for col > 0 && row[col].Occupied && row[col].Width == 0 {
		col--
	}
	return col
}

func cellsToString(row []Cell) string {
	var out strings.Builder
	for _, cell := range row {
		if cell.Occupied && cell.Width == 0 {
			continue
		}
		if !cell.Occupied {
			out.WriteRune(' ')
			continue
		}
		ch := cell.Ch
		if ch == 0 {
			ch = ' '
		}
		out.WriteRune(ch)
		if len(cell.Comb) > 0 {
			out.WriteString(string(cell.Comb))
		}
	}
	return out.String()
}

func selectionStyle(style tcell.Style) tcell.Style {
	fg, bg, attr := style.Decompose()
	if fg == tcell.ColorDefault && bg == tcell.ColorDefault {
		return tcell.StyleDefault.Foreground(tcell.ColorBlack).Background(tcell.ColorWhite).Attributes(attr)
	}
	return tcell.StyleDefault.Foreground(bg).Background(fg).Attributes(attr)
}

func scrollbarThumb(height, rows, offset int) (start, size int) {
	if height <= 0 {
		return 0, 0
	}
	if rows <= 0 {
		return 0, height
	}

	totalRows := rows + height
	size = int(math.Round(float64(height*height) / float64(totalRows)))
	size = clamp(size, 1, height)
	maxStart := max(0, height-size)
	if rows == 0 || maxStart == 0 {
		return 0, size
	}

	ratio := float64(rows-offset) / float64(rows)
	start = int(math.Round(ratio * float64(maxStart)))
	start = clamp(start, 0, maxStart)
	return start, size
}

func (v *View) handleScrollbarMouse(action tview.MouseAction, event *tcell.EventMouse, setFocus func(p tview.Primitive)) bool {
	x, y, width, height := v.GetInnerRect()
	contentWidth, scrollbarX, showScrollbar := v.layout(width)
	if !showScrollbar || contentWidth <= 0 {
		return false
	}

	mouseX, mouseY := event.Position()
	if mouseX != x+scrollbarX || mouseY < y || mouseY >= y+height {
		return false
	}

	switch action {
	case tview.MouseLeftDown:
		setFocus(v)
		v.scrollbarJumpTo(mouseY - y)
		return true
	case tview.MouseMove:
		if event.Buttons()&tcell.Button1 != 0 {
			v.scrollbarJumpTo(mouseY - y)
			return true
		}
	case tview.MouseScrollUp:
		v.ScrollbackUp(3)
		return true
	case tview.MouseScrollDown:
		v.ScrollbackDown(3)
		return true
	}
	return false
}

func (v *View) scrollbarJumpTo(row int) {
	_, viewRows, scrollbackRows := v.emu.Dimensions()
	if scrollbackRows <= 0 || viewRows <= 0 {
		v.scrollToBottom()
		return
	}

	v.mu.RLock()
	scrollOffset := v.scrollOffset
	v.mu.RUnlock()
	_, thumbHeight := scrollbarThumb(viewRows, scrollbackRows, scrollOffset)
	maxStart := max(0, viewRows-thumbHeight)
	if maxStart == 0 {
		v.scrollToBottom()
		return
	}

	targetStart := clamp(row-thumbHeight/2, 0, maxStart)
	ratio := float64(targetStart) / float64(maxStart)
	targetOffset := scrollbackRows - int(math.Round(ratio*float64(scrollbackRows)))

	v.mu.Lock()
	v.scrollOffset = clamp(targetOffset, 0, scrollbackRows)
	v.mu.Unlock()
	v.requestRedraw()
}

func (v *View) syncTitle() {
	title := v.emu.WindowTitle()
	if title == "" {
		return
	}

	v.mu.Lock()
	if title == v.lastTitle {
		v.mu.Unlock()
		return
	}
	v.lastTitle = title
	handler := v.onTitle
	v.mu.Unlock()
	if handler != nil {
		handler(v, title)
	}
}

func (v *View) reportFocus(focused bool) {
	v.mu.RLock()
	backend := v.backend
	v.mu.RUnlock()
	if backend == nil {
		return
	}

	if !v.emu.FocusReporting() {
		return
	}
	if focused {
		v.sendInput(backend, []byte("\x1b[I"))
		return
	}
	v.sendInput(backend, []byte("\x1b[O"))
}

func (v *View) logBytes(dir string, p []byte) {
	v.mu.RLock()
	debug := v.debug
	v.mu.RUnlock()
	if debug == nil {
		return
	}
	_, _ = fmt.Fprintf(debug, "%s %q\n", dir, p)
}

func (v *View) sendInput(backend Backend, payload []byte) {
	if backend == nil || len(payload) == 0 {
		return
	}
	v.resetScrollback()
	v.logBytes("input", payload)
	_ = writeAll(backend, payload)
}

func drawStyle(style tcell.Style, reverseVideo bool) tcell.Style {
	if !reverseVideo {
		return style
	}
	fg, bg, attr := style.Decompose()
	return tcell.StyleDefault.Foreground(bg).Background(fg).Attributes(attr)
}

func (v *View) sendMouse(action tview.MouseAction, event *tcell.EventMouse) {
	v.mu.RLock()
	backend := v.backend
	v.mu.RUnlock()
	if backend == nil || event == nil {
		return
	}

	x, y, _, _ := v.GetInnerRect()
	ss := v.emu.Snapshot()
	seq, ok := mouseEventToBytes(action, event, ss, x, y)
	if !ok {
		return
	}
	v.sendInput(backend, seq)
}

func mouseEventToBytes(action tview.MouseAction, event *tcell.EventMouse, ss Snapshot, originX, originY int) ([]byte, bool) {
	if !mouseReportingEnabled(ss) {
		return nil, false
	}

	x, y := event.Position()
	col := x - originX + 1
	row := y - originY + 1
	if col < 1 || row < 1 {
		return nil, false
	}

	btn, suffix, ok := mouseReportCode(action, event, ss)
	if !ok {
		return nil, false
	}
	btn += mouseModifierBits(event.Modifiers())

	if ss.MouseSGR {
		return []byte(fmt.Sprintf("\x1b[<%d;%d;%d%c", btn, col, row, suffix)), true
	}
	if col > 223 || row > 223 {
		return nil, false
	}
	return []byte{0x1b, '[', 'M', byte(btn + 32), byte(col + 32), byte(row + 32)}, true
}

func mouseReportingEnabled(ss Snapshot) bool {
	return ss.MouseX10 || ss.MouseVT200 || ss.MouseButtonEvt || ss.MouseAnyEvt
}

func mouseReportCode(action tview.MouseAction, event *tcell.EventMouse, ss Snapshot) (btn int, suffix rune, ok bool) {
	switch action {
	case tview.MouseLeftDown:
		return 0, 'M', true
	case tview.MouseMiddleDown:
		return 1, 'M', true
	case tview.MouseRightDown:
		return 2, 'M', true
	case tview.MouseLeftUp, tview.MouseMiddleUp, tview.MouseRightUp:
		if ss.MouseX10 {
			return 0, 0, false
		}
		if ss.MouseSGR {
			switch action {
			case tview.MouseLeftUp:
				return 0, 'm', true
			case tview.MouseMiddleUp:
				return 1, 'm', true
			case tview.MouseRightUp:
				return 2, 'm', true
			}
		}
		return 3, 'm', true
	case tview.MouseScrollUp:
		if ss.MouseX10 {
			return 0, 0, false
		}
		return 64, 'M', true
	case tview.MouseScrollDown:
		if ss.MouseX10 {
			return 0, 0, false
		}
		return 65, 'M', true
	case tview.MouseScrollLeft:
		if ss.MouseX10 {
			return 0, 0, false
		}
		return 66, 'M', true
	case tview.MouseScrollRight:
		if ss.MouseX10 {
			return 0, 0, false
		}
		return 67, 'M', true
	case tview.MouseMove:
		if !ss.MouseAnyEvt && !(ss.MouseButtonEvt && mousePressedButton(event.Buttons()) >= 0) {
			return 0, 0, false
		}
		base := mousePressedButton(event.Buttons())
		if base < 0 {
			base = 3
		}
		return base + 32, 'M', true
	default:
		return 0, 0, false
	}
}

func mousePressedButton(mask tcell.ButtonMask) int {
	switch {
	case mask&tcell.Button1 != 0:
		return 0
	case mask&tcell.Button2 != 0:
		return 1
	case mask&tcell.Button3 != 0:
		return 2
	default:
		return -1
	}
}

func mouseModifierBits(mod tcell.ModMask) int {
	var bits int
	if mod&tcell.ModShift != 0 {
		bits |= 4
	}
	if mod&tcell.ModAlt != 0 {
		bits |= 8
	}
	if mod&tcell.ModCtrl != 0 {
		bits |= 16
	}
	return bits
}

// resizeBackend forwards a size change to the backend. It is a no-op unless
// the size actually changed — Draw runs per frame, and without this guard
// every frame would issue a PTY TIOCSWINSZ ioctl even when idle.
func (v *View) resizeBackend(cols, rows int) {
	v.mu.Lock()
	if v.lastBackendCols == cols && v.lastBackendRows == rows {
		v.mu.Unlock()
		return
	}
	v.lastBackendCols = cols
	v.lastBackendRows = rows
	backend := v.backend
	v.mu.Unlock()
	if backend != nil {
		_ = backend.Resize(cols, rows)
	}
}

func keyToBytes(ev *tcell.EventKey, appCursor bool) ([]byte, bool) {
	if seq, ok := modifiedKeyToBytes(ev); ok {
		return seq, true
	}

	switch ev.Key() {
	case tcell.KeyRune:
		return []byte(string(ev.Rune())), true
	case tcell.KeyEnter:
		return []byte("\r"), true
	case tcell.KeyTab:
		return []byte("\t"), true
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		// tcell ≥2.10 normalizes KeyBackspace2 to KeyBackspace; DEL (0x7f) is
		// the modern erase character expected by ssh servers and readline.
		return []byte{0x7f}, true
	case tcell.KeyEsc:
		return []byte{0x1b}, true
	case tcell.KeyHome:
		if appCursor {
			return []byte("\x1bOH"), true
		}
		return []byte("\x1b[H"), true
	case tcell.KeyEnd:
		if appCursor {
			return []byte("\x1bOF"), true
		}
		return []byte("\x1b[F"), true
	case tcell.KeyDelete:
		return []byte("\x1b[3~"), true
	case tcell.KeyInsert:
		return []byte("\x1b[2~"), true
	case tcell.KeyPgUp:
		return []byte("\x1b[5~"), true
	case tcell.KeyPgDn:
		return []byte("\x1b[6~"), true
	case tcell.KeyBacktab:
		return []byte("\x1b[Z"), true
	case tcell.KeyUp:
		if appCursor {
			return []byte("\x1bOA"), true
		}
		return []byte("\x1b[A"), true
	case tcell.KeyDown:
		if appCursor {
			return []byte("\x1bOB"), true
		}
		return []byte("\x1b[B"), true
	case tcell.KeyRight:
		if appCursor {
			return []byte("\x1bOC"), true
		}
		return []byte("\x1b[C"), true
	case tcell.KeyLeft:
		if appCursor {
			return []byte("\x1bOD"), true
		}
		return []byte("\x1b[D"), true
	case tcell.KeyCtrlC:
		return []byte{0x03}, true
	case tcell.KeyCtrlD:
		return []byte{0x04}, true
	case tcell.KeyCtrlL:
		return []byte{0x0c}, true
	default:
		return nil, false
	}
}

func modifiedKeyToBytes(ev *tcell.EventKey) ([]byte, bool) {
	mod := ev.Modifiers()

	if ev.Key() >= tcell.KeyCtrlA && ev.Key() <= tcell.KeyCtrlZ {
		return []byte{byte(ev.Key() - tcell.KeyCtrlA + 1)}, true
	}

	switch ev.Key() {
	case tcell.KeyCtrlSpace:
		return []byte{0x00}, true
	}

	if mod&tcell.ModCtrl != 0 {
		switch ev.Key() {
		case tcell.KeyLeft:
			return []byte("\x1b[1;5D"), true
		case tcell.KeyRight:
			return []byte("\x1b[1;5C"), true
		case tcell.KeyDelete:
			return []byte("\x1b[3;5~"), true
		case tcell.KeyBackspace, tcell.KeyBackspace2:
			return []byte{0x17}, true
		}
	}

	if mod&tcell.ModAlt != 0 {
		switch ev.Key() {
		case tcell.KeyBackspace, tcell.KeyBackspace2:
			// tcell ≥2.10 normalizes KeyBackspace2 to KeyBackspace; both mean
			// "backspace" on modern terminals, and the shells' default
			// backward-kill-word binding is ESC DEL.
			return []byte{0x1b, 0x7f}, true
		case tcell.KeyLeft:
			return []byte{0x1b, 'b'}, true
		case tcell.KeyRight:
			return []byte{0x1b, 'f'}, true
		case tcell.KeyDelete:
			return []byte{0x1b, 'd'}, true
		case tcell.KeyUp:
			return []byte("\x1b[1;3A"), true
		case tcell.KeyDown:
			return []byte("\x1b[1;3B"), true
		case tcell.KeyRune:
			buf := make([]byte, 1+utf8.RuneLen(ev.Rune()))
			buf[0] = 0x1b
			n := utf8.EncodeRune(buf[1:], ev.Rune())
			return buf[:1+n], true
		}
	}

	return nil, false
}

func writeAll(w io.Writer, p []byte) error {
	for len(p) > 0 {
		n, err := w.Write(p)
		if n > 0 {
			p = p[n:]
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}
