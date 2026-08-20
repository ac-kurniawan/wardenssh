package tviewui

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// ScopeModal is the Ctrl+B overlay: pick a host scope/group. Rows show a
// checkmark for the current scope, a friendly label, and the host count.
// Navigation: ↑/↓ move, Enter selects, Esc cancels.
type ScopeModal struct {
	modal    *tview.Flex
	list     *tview.List
	scopes   []string
	onSelect func(string)
	onCancel func()
}

// NewScopeModal builds the scope-picker modal. scopes is the ordered scope
// cycle from hosts.List.Scopes(); counts maps each scope to its launchable
// entry count (hosts.List.CountInScope); current is the active scope.
func NewScopeModal(scopes []string, counts map[string]int, current string) *ScopeModal {
	m := &ScopeModal{scopes: scopes}
	m.list = tview.NewList().ShowSecondaryText(false)
	m.list.SetBorder(true)
	m.list.SetTitle(" Select Host Scope / Group ")
	m.list.SetTitleAlign(tview.AlignLeft)
	for i, s := range scopes {
		label := scopeModalLabel(s)
		cnt := counts[s]
		mark := "[ ]"
		if s == current {
			mark = "[*]"
		}
		text := fmt.Sprintf(" %s %-20s (%d) ", mark, label, cnt)
		m.list.AddItem(text, "", 0, nil)
		if s == current {
			m.list.SetCurrentItem(i)
		}
	}
	m.list.SetSelectedStyle(tcell.StyleDefault.
		Background(SelectionBG).
		Foreground(SelectionFG).
		Bold(true))
	m.list.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEnter:
			m.triggerSelect()
			return nil
		case tcell.KeyEscape:
			m.triggerCancel()
			return nil
		}
		return event
	})

	inner := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(nil, 0, 1, false).
		AddItem(m.list, 30, 0, true).
		AddItem(nil, 0, 1, false)
	m.modal = tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(nil, 0, 1, false).
		AddItem(inner, 0, 1, true).
		AddItem(nil, 0, 1, false)
	return m
}

// scopeModalLabel maps a scope to its friendly picker label ("All Hosts" for
// the all-scope, "~/.ssh/config" for the file source, vault names as-is).
func scopeModalLabel(scope string) string {
	switch scope {
	case "":
		return "All Hosts"
	case "file":
		return "~/.ssh/config"
	default:
		return scope
	}
}

// Primitive returns the tview primitive for layout embedding.
func (m *ScopeModal) Primitive() tview.Primitive { return m.modal }

// SetOnSelect installs the callback fired when a scope is chosen.
func (m *ScopeModal) SetOnSelect(fn func(string)) { m.onSelect = fn }

// SetOnCancel installs the callback fired on Escape.
func (m *ScopeModal) SetOnCancel(fn func()) { m.onCancel = fn }

// Current returns the focused row's text (used in tests).
func (m *ScopeModal) Current() string {
	idx := m.list.GetCurrentItem()
	if idx < 0 || idx >= m.list.GetItemCount() {
		return ""
	}
	main, _ := m.list.GetItemText(idx)
	return main
}

// TriggerSelect fires the select callback for the highlighted scope.
func (m *ScopeModal) TriggerSelect() { m.triggerSelect() }

// TriggerCancel fires the cancel callback.
func (m *ScopeModal) TriggerCancel() { m.triggerCancel() }

// TriggerKey routes a key event through the modal's list input capture (tests).
func (m *ScopeModal) TriggerKey(event *tcell.EventKey) {
	if h := m.list.InputHandler(); h != nil {
		h(event, func(tview.Primitive) {})
	}
}

func (m *ScopeModal) triggerSelect() {
	idx := m.list.GetCurrentItem()
	if idx < 0 || idx >= len(m.scopes) {
		return
	}
	if m.onSelect != nil {
		m.onSelect(m.scopes[idx])
	}
}

func (m *ScopeModal) triggerCancel() {
	if m.onCancel != nil {
		m.onCancel()
	}
}