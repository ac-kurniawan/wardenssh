package tviewui

import (
	"fmt"

	tvxterm "github.com/ac-kurniawan/wardenssh/third_party/tvxterm"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// terminalView wraps a tvxterm.View with the WardenSSH terminal interaction
// model:
//
//   - mouse-wheel over the view scrolls the local scrollback (never forwarded
//     to the remote app, which would interpret it as arrow-key navigation);
//   - primary-button click-hold-drag selects text locally and copies it to the
//     OS clipboard on release.
//
// All other mouse events are delegated to the embedded view unchanged, so
// remote mouse-reporting apps (tmux/vim/less) still receive clicks and the
// built-in scrollbar keeps working.
type terminalView struct {
	*tvxterm.View
	dragging             bool // a primary-button selection drag is in progress
	lastDragX, lastDragY int
}

// Dragging reports whether a primary-button text selection is in progress.
func (s *terminalView) Dragging() bool {
	if s == nil {
		return false
	}
	return s.dragging
}

// newTerminalView builds a WardenSSH-wired tvxterm.View. The app reference is
// used for focus handling; it may be nil for tests.
func newTerminalView(app *tview.Application, title string) *terminalView {
	term := &terminalView{View: tvxterm.New(app)}
	term.SetBorder(true).SetTitle(fmt.Sprintf(" %s ", title))
	term.SetScrollbar(true)
	return term
}

// MouseHandler routes mouse events for the terminal:
//
//   - wheel -> local scrollback scrolling; during a selection drag the wheel
//     also extends the highlight into the newly revealed lines instead of
//     clearing it;
//   - primary-button drag -> local text selection (copied to the clipboard on
//     release). Dragging past the top or bottom edge scrolls the local
//     scrollback. During a drag the view captures subsequent mouse events so
//     selection keeps tracking even when the pointer leaves the view;
//   - everything else -> the embedded tvxterm handler (scrollbar, remote mouse
//     reporting, focus-on-click).
func (s *terminalView) MouseHandler() func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(p tview.Primitive)) (bool, tview.Primitive) {
	orig := s.View.MouseHandler()
	return func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(p tview.Primitive)) (bool, tview.Primitive) {
		if event == nil {
			return orig(action, event, setFocus)
		}

		// During an active selection drag keep tracking move/release even when
		// the pointer leaves the view (tview routes to the returned capture).
		if s.dragging {
			switch action {
			case tview.MouseMove:
				if event.Buttons()&tcell.Button1 != 0 {
					s.dragSelect(event.Position())
					return true, s
				}
				// A wheel tick also arrives as a move, and that move carries
				// no button. Keep the capture so the wheel action that tview
				// fires next on the same event still reaches this view.
				return true, s
			case tview.MouseLeftUp:
				// tcell reports a wheel tick during a held button as a
				// mouse-up whose mask is the wheel. The real scroll action
				// follows on the same tick, so this one must not end the drag.
				if event.Buttons()&(tcell.WheelUp|tcell.WheelDown) != 0 {
					return true, s
				}
				s.dragging = false
				s.finishSelection()
				return true, nil
			case tview.MouseScrollUp:
				s.scrollSelection(setFocus, -1)
				return true, s
			case tview.MouseScrollDown:
				s.scrollSelection(setFocus, 1)
				return true, s
			}
			// Anything not handled above is ignored while the button is held.
			// Delegating to the embedded handler is what turns the wheel into a
			// mouse-up and ends the drag.
			return true, s
		}

		x, y := event.Position()
		// A press is delivered as a move first, then the down, on the same
		// event. The move's answer is what tview keeps as the capture, so a
		// move that still holds the button must name this view or the down
		// that follows is the last event the view ever sees.
		if action == tview.MouseMove && event.Buttons()&tcell.Button1 != 0 && s.InRect(x, y) {
			if !s.onScrollbarColumn(x, y) {
				s.dragging = true
				s.lastDragX, s.lastDragY = x, y
				s.StartSelection(x, y)
			}
			return true, s
		}
		if !s.InRect(x, y) {
			return orig(action, event, setFocus)
		}

		switch action {
		case tview.MouseScrollUp:
			s.scrollLocal(setFocus, -1)
			return true, nil
		case tview.MouseScrollDown:
			s.scrollLocal(setFocus, 1)
			return true, nil
		case tview.MouseLeftDown:
			if s.onScrollbarColumn(x, y) {
				// Let the embedded handler drive the scrollbar (jump/drag).
				return orig(action, event, setFocus)
			}
			s.dragging = true
			s.lastDragX, s.lastDragY = x, y
			s.StartSelection(x, y)
			if !s.HasFocus() {
				setFocus(s)
			}
			return true, s
		}
		return orig(action, event, setFocus)
	}
}

// scrollLocal scrolls the pane's own scrollback. dir < 0 moves toward older
// lines. An already-focused view is not refocused: Focus() reports focus-in
// to the remote, and that input path resets the scroll offset.
func (s *terminalView) scrollLocal(setFocus func(p tview.Primitive), dir int) {
	if !s.HasFocus() {
		setFocus(s)
	}
	if dir < 0 {
		s.ScrollbackUp(3)
		return
	}
	s.ScrollbackDown(3)
}

// scrollSelection scrolls during a held selection and re-anchors the highlight
// on the pointer so the newly revealed lines join the selection.
func (s *terminalView) scrollSelection(setFocus func(p tview.Primitive), dir int) {
	before, _ := s.ScrollbackStatus()
	s.scrollLocal(setFocus, dir)
	after, _ := s.ScrollbackStatus()
	x, y := s.lastDragX, s.lastDragY
	_, iy, _, ih := s.GetInnerRect()
	if after != before && ih > 0 {
		// Wheel moves the buffer under a stationary pointer. Shift the tracked
		// row by the same number of lines so the selection follows the content
		// that just scrolled into view.
		y += after - before
		if y < iy {
			y = iy
		}
		if y >= iy+ih {
			y = iy + ih - 1
		}
		s.lastDragY = y
	}
	s.UpdateSelection(x, y)
}

// dragSelect updates the selection and, when the pointer is outside the
// content area, scrolls the local scrollback in that direction.
func (s *terminalView) dragSelect(x, y int) {
	s.lastDragX, s.lastDragY = x, y
	ix, iy, iw, ih := s.GetInnerRect()
	// Scrollbar column is not selectable text.
	contentRight := ix + iw
	if iw >= 2 {
		contentRight = ix + iw - 1
	}
	clampedX, clampedY := x, y
	switch {
	case ih > 0 && y < iy:
		s.ScrollbackUp(1)
		clampedY = iy
	case ih > 0 && y >= iy+ih:
		s.ScrollbackDown(1)
		clampedY = iy + ih - 1
	}
	if iw > 0 {
		if clampedX < ix {
			clampedX = ix
		}
		if clampedX >= contentRight && contentRight > ix {
			clampedX = contentRight - 1
		}
	}
	s.UpdateSelection(clampedX, clampedY)
}

// onScrollbarColumn reports whether (x, y) is on the visible scrollbar column
// (the last column of the inner area when the scrollbar is enabled).
func (s *terminalView) onScrollbarColumn(x, y int) bool {
	ix, iy, iw, ih := s.GetInnerRect()
	if iw < 2 || ih <= 0 {
		return false
	}
	return x == ix+iw-1 && y >= iy && y < iy+ih
}

// finishSelection finalizes a completed drag: a real selection is kept
// highlighted and copied to the clipboard; a zero-length selection (plain
// click) is cleared.
func (s *terminalView) finishSelection() {
	if !s.HasSelection() {
		s.ClearSelection()
		return
	}
	if text := s.SelectedText(); text != "" {
		_ = copySelection(text)
	}
}

// CopySelection copies the current local text selection to the OS clipboard
// and clears it, reporting whether a selection was present.
func (s *terminalView) CopySelection() bool {
	if !s.HasSelection() {
		return false
	}
	if text := s.SelectedText(); text != "" {
		_ = copySelection(text)
	}
	s.ClearSelection()
	return true
}

// copySelection writes selected terminal text to the OS clipboard. It is a
// package variable so tests can capture what would be copied.
var copySelection = func(text string) error {
	return copyToClipboard(text)
}
