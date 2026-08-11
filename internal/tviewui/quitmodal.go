package tviewui

import (
	"github.com/rivo/tview"
)

// QuitModal is the quit-confirmation modal (Q31/C). Shown when the user
// presses 'q' or Ctrl+C while live sessions exist. Options: Kill all (default),
// Detach, Cancel.
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
			case 2:
				q.triggerCancel()
			}
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
