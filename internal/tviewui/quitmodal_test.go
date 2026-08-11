package tviewui_test

import (
	"testing"

	"github.com/ac-kurniawan/wardenssh/internal/tviewui"
)

func TestQuitModalKillAll(t *testing.T) {
	m := tviewui.NewQuitModal()
	var action string
	m.SetOnKillAll(func() { action = "killall" })
	m.TriggerKillAll()
	if action != "killall" {
		t.Errorf("expected killall, got %q", action)
	}
}

func TestQuitModalDetach(t *testing.T) {
	m := tviewui.NewQuitModal()
	var action string
	m.SetOnDetach(func() { action = "detach" })
	m.TriggerDetach()
	if action != "detach" {
		t.Errorf("expected detach, got %q", action)
	}
}

func TestQuitModalCancel(t *testing.T) {
	m := tviewui.NewQuitModal()
	var action string
	m.SetOnCancel(func() { action = "cancel" })
	m.TriggerCancel()
	if action != "cancel" {
		t.Errorf("expected cancel, got %q", action)
	}
}
