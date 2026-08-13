package tviewui

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// DisconnectModal asks the user to confirm disconnecting a host session
// (Q18/iii: selecting the active host again = disconnect, with confirmation).
// Options: Disconnect (y/Enter), Cancel (n/Escape).
type DisconnectModal struct {
	modal         *tview.Modal
	onDisconnect  func()
	onCancel      func()
}

// NewDisconnectModal builds the disconnect-confirmation modal for a host alias.
func NewDisconnectModal(alias string) *DisconnectModal {
	m := &DisconnectModal{}
	m.modal = tview.NewModal().
		SetText(fmt.Sprintf("Disconnect session to %s?\n\n[y] Disconnect\n[n] Cancel", alias)).
		AddButtons([]string{"Disconnect", "Cancel"}).
		SetDoneFunc(func(buttonIndex int, buttonLabel string) {
			switch buttonIndex {
			case 0:
				m.triggerDisconnect()
			case 1, -1: // -1 = Escape pressed in the modal
				m.triggerCancel()
			}
		})
	m.modal.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch {
		case event.Key() == tcell.KeyEscape:
			m.triggerCancel()
			return nil
		case event.Rune() == 'y':
			m.triggerDisconnect()
			return nil
		case event.Rune() == 'n':
			m.triggerCancel()
			return nil
		}
		return event
	})
	return m
}

// Primitive returns the tview primitive for layout embedding.
func (m *DisconnectModal) Primitive() tview.Primitive { return m.modal }

// SetOnDisconnect installs the confirm callback.
func (m *DisconnectModal) SetOnDisconnect(fn func()) { m.onDisconnect = fn }

// SetOnCancel installs the cancel callback.
func (m *DisconnectModal) SetOnCancel(fn func()) { m.onCancel = fn }

// TriggerDisconnect fires the confirm callback (tests / modal 'y').
func (m *DisconnectModal) TriggerDisconnect() { m.triggerDisconnect() }

// TriggerCancel fires the cancel callback (tests / modal 'n').
func (m *DisconnectModal) TriggerCancel() { m.triggerCancel() }

func (m *DisconnectModal) triggerDisconnect() {
	if m.onDisconnect != nil {
		m.onDisconnect()
	}
}

func (m *DisconnectModal) triggerCancel() {
	if m.onCancel != nil {
		m.onCancel()
	}
}
