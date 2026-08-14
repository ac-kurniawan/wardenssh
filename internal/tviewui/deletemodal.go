package tviewui

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// DeleteModal asks the user to confirm deleting an SSH host connection entry.
type DeleteModal struct {
	modal    *tview.Modal
	onDelete func()
	onCancel func()
}

// NewDeleteModal builds the delete-confirmation modal for a host entry.
func NewDeleteModal(alias string, source string) *DeleteModal {
	m := &DeleteModal{}
	targetDesc := "~/.ssh/config"
	if source != "file" && source != "~/.ssh/config" {
		targetDesc = fmt.Sprintf("Vault (%s)", source)
	}

	m.modal = tview.NewModal().
		SetText(fmt.Sprintf("Delete connection '%s' from %s?\n\nThis action cannot be undone.\n\n[y] Delete\n[n] Cancel", alias, targetDesc)).
		AddButtons([]string{"Delete", "Cancel"}).
		SetDoneFunc(func(buttonIndex int, buttonLabel string) {
			switch buttonIndex {
			case 0:
				m.triggerDelete()
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
			m.triggerDelete()
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
func (m *DeleteModal) Primitive() tview.Primitive { return m.modal }

// SetOnDelete installs the delete confirmation callback.
func (m *DeleteModal) SetOnDelete(fn func()) { m.onDelete = fn }

// SetOnCancel installs the cancel callback.
func (m *DeleteModal) SetOnCancel(fn func()) { m.onCancel = fn }

// TriggerDelete fires the delete callback.
func (m *DeleteModal) TriggerDelete() { m.triggerDelete() }

// TriggerCancel fires the cancel callback.
func (m *DeleteModal) TriggerCancel() { m.triggerCancel() }

func (m *DeleteModal) triggerDelete() {
	if m.onDelete != nil {
		m.onDelete()
	}
}

func (m *DeleteModal) triggerCancel() {
	if m.onCancel != nil {
		m.onCancel()
	}
}
