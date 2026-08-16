package tviewui

import (
	"fmt"

	"github.com/blacknon/tvxterm"
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
	dragging bool // a primary-button selection drag is in progress
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
//   - wheel -> local scrollback scrolling;
//   - primary-button drag -> local text selection (copied to the clipboard on
//     release); during a drag the view captures subsequent mouse events so
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
					s.UpdateSelection(event.Position())
					return true, s
				}
				// Button released without a MouseLeftUp (rare); stop dragging.
				s.dragging = false
			case tview.MouseLeftUp:
				s.dragging = false
				s.finishSelection()
				return true, nil
		case tview.MouseScrollUp:
			if !s.HasFocus() {
				setFocus(s)
			}
			s.ScrollbackUp(3)
			return true, s
		case tview.MouseScrollDown:
			if !s.HasFocus() {
				setFocus(s)
			}
			s.ScrollbackDown(3)
			return true, s
		}
		return orig(action, event, setFocus)
	}

	x, y := event.Position()
	if !s.InRect(x, y) {
		return orig(action, event, setFocus)
	}

	switch action {
	case tview.MouseScrollUp:
		if !s.HasFocus() {
			setFocus(s)
		}
		s.ScrollbackUp(3)
		return true, nil
	case tview.MouseScrollDown:
		if !s.HasFocus() {
			setFocus(s)
		}
		s.ScrollbackDown(3)
		return true, nil
		case tview.MouseLeftDown:
			if s.onScrollbarColumn(x, y) {
				// Let the embedded handler drive the scrollbar (jump/drag).
				return orig(action, event, setFocus)
			}
			s.dragging = true
			s.StartSelection(x, y)
			setFocus(s)
			return true, s
		}
		return orig(action, event, setFocus)
	}
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
