package tviewui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// QuitModal is the quit-confirmation modal (Q31/C). Shown when the user
// presses 'q', Ctrl+C, or Escape at the host list. Options (keybindings in
// parentheses): Kill all (k), Detach (d), Cancel (c/Escape).
type QuitModal struct {
	modal     *tview.Modal
	onKillAll func()
	onDetach  func()
	onCancel  func()
}

// NewQuitModal builds the quit confirmation modal.
func NewQuitModal() *QuitModal {
	q := &QuitModal{}
	q.modal = tview.NewModal().
		SetText("Quit WardenSSH?\n\n[k] Kill all sessions & quit (default)\n[d] Detach sessions & quit\n[c] Cancel").
		AddButtons([]string{"Kill all", "Detach", "Cancel"}).
		SetDoneFunc(func(buttonIndex int, buttonLabel string) {
			switch buttonIndex {
			case 0:
				q.triggerKillAll()
			case 1:
				q.triggerDetach()
			case 2, -1: // -1 = Escape pressed in the modal
				q.triggerCancel()
			}
		})
	// Keyboard shortcuts k/d/c/Escape captured before the modal's default
	// handler (which only acts when the frame has focus).
	q.modal.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch {
		case event.Key() == tcell.KeyEscape:
			q.triggerCancel()
			return nil
		case event.Rune() == 'k':
			q.triggerKillAll()
			return nil
		case event.Rune() == 'd':
			q.triggerDetach()
			return nil
		case event.Rune() == 'c':
			q.triggerCancel()
			return nil
		}
		return event
	})
	return q
}

// Primitive returns the tview primitive for layout embedding.
func (q *QuitModal) Primitive() tview.Primitive { return q.modal }

// SetOnKillAll installs the Kill all callback.
func (q *QuitModal) SetOnKillAll(fn func()) { q.onKillAll = fn }

// SetOnDetach installs the Detach callback.
func (q *QuitModal) SetOnDetach(fn func()) { q.onDetach = fn }

// SetOnCancel installs the Cancel callback.
func (q *QuitModal) SetOnCancel(fn func()) { q.onCancel = fn }

// TriggerKillAll fires the Kill all callback (for tests).
func (q *QuitModal) TriggerKillAll() { q.triggerKillAll() }

// TriggerDetach fires the Detach callback (for tests).
func (q *QuitModal) TriggerDetach() { q.triggerDetach() }

// TriggerCancel fires the Cancel callback (for tests).
func (q *QuitModal) TriggerCancel() { q.triggerCancel() }

// TriggerKey routes a key event through the modal's input capture (tests).
func (q *QuitModal) TriggerKey(event *tcell.EventKey) {
	if h := q.modal.InputHandler(); h != nil {
		h(event, func(tview.Primitive) {})
	}
}

func (q *QuitModal) triggerKillAll() {
	if q.onKillAll != nil {
		q.onKillAll()
	}
}
func (q *QuitModal) triggerDetach() {
	if q.onDetach != nil {
		q.onDetach()
	}
}
func (q *QuitModal) triggerCancel() {
	if q.onCancel != nil {
		q.onCancel()
	}
}
