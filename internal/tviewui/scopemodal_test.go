package tviewui_test

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/ac-kurniawan/wardenssh/internal/tviewui"
)

func TestScopeModalListsScopesWithCounts(t *testing.T) {
	m := tviewui.NewScopeModal([]string{"", "vw:work", "file"}, map[string]int{"": 7, "vw:work": 3, "file": 2}, "")
	text := m.Current()
	if text == "" {
		t.Fatal("expected a current scope row")
	}
	if !strings.Contains(text, "7") || !strings.Contains(text, "All") {
		t.Errorf("all-scope row should show count 7 and label All: %q", text)
	}
}

func TestScopeModalSelectCallsBack(t *testing.T) {
	m := tviewui.NewScopeModal([]string{"", "file"}, map[string]int{"": 7, "file": 2}, "")
	var got string
	m.SetOnSelect(func(s string) { got = s })
	// Move down to "file", select it.
	m.TriggerKey(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone))
	m.TriggerSelect()
	if got != "file" {
		t.Errorf("selected = %q, want file", got)
	}
}

func TestScopeModalCancel(t *testing.T) {
	m := tviewui.NewScopeModal([]string{"", "file"}, map[string]int{"": 7, "file": 2}, "")
	cancelled := false
	m.SetOnCancel(func() { cancelled = true })
	m.TriggerCancel()
	if !cancelled {
		t.Error("expected cancel callback")
	}
}

func TestScopeModalMarksCurrentScope(t *testing.T) {
	m := tviewui.NewScopeModal([]string{"", "file"}, map[string]int{"": 7, "file": 2}, "file")
	if !strings.Contains(m.Current(), "[*]") {
		t.Errorf("current scope row should be checked: %q", m.Current())
	}
}