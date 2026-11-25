package tviewui

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// CopyModal asks the user to confirm copying a host entry between the local
// ssh config and a vault. direction is the human-readable arrow label
// ("vault → local" / "local → vault"); warning states what the copy touches.
type CopyModal struct {
	modal     *tview.Modal
	onCopy    func()
	onCancel  func()
	alias     string
	direction string
	body      string
}

// NewCopyModal builds the copy-confirmation modal for a host entry.
func NewCopyModal(alias, direction, targetDesc, warning string) *CopyModal {
	m := &CopyModal{alias: alias, direction: direction}

	m.body = fmt.Sprintf("Copy '%s' %s to %s?", alias, direction, targetDesc)
	if warning != "" {
		m.body += "\n\n" + warning
	}
	m.body += "\n\n[y] Copy\n[n] Cancel"

	m.modal = tview.NewModal().
		SetText(m.body).
		AddButtons([]string{"Copy", "Cancel"}).
		SetDoneFunc(func(buttonIndex int, buttonLabel string) {
			switch buttonIndex {
			case 0:
				m.triggerCopy()
			case 1, -1: // -1 = Escape pressed in modal
				m.triggerCancel()
			}
		})

	m.modal.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch {
		case event.Key() == tcell.KeyEscape:
			m.triggerCancel()
			return nil
		case event.Rune() == 'y' || event.Rune() == 'Y':
			m.triggerCopy()
			return nil
		case event.Rune() == 'n' || event.Rune() == 'N':
			m.triggerCancel()
			return nil
		}
		return event
	})

	return m
}

// Primitive returns the tview primitive for layout embedding.
func (m *CopyModal) Primitive() tview.Primitive { return m.modal }

// Text returns the modal body (used in tests).
func (m *CopyModal) Text() string { return m.body }

// SetOnCopy installs the copy confirmation callback.
func (m *CopyModal) SetOnCopy(fn func()) { m.onCopy = fn }

// SetOnCancel installs the cancel callback.
func (m *CopyModal) SetOnCancel(fn func()) { m.onCancel = fn }

// TriggerCopy fires the copy callback.
func (m *CopyModal) TriggerCopy() { m.triggerCopy() }

// TriggerCancel fires the cancel callback.
func (m *CopyModal) TriggerCancel() { m.triggerCancel() }

// CopyTargetAlias returns the alias of the entry this modal was built for.
func (m *CopyModal) CopyTargetAlias() string { return m.alias }

// CopyDirection returns the direction label this modal was built for.
func (m *CopyModal) CopyDirection() string { return m.direction }

func (m *CopyModal) triggerCopy() {
	if m.onCopy != nil {
		m.onCopy()
	}
}

func (m *CopyModal) triggerCancel() {
	if m.onCancel != nil {
		m.onCancel()
	}
}
