package tviewui_test

import (
	"strings"
	"testing"

	"github.com/ac-kurniawan/wardenssh/internal/tviewui"
)

func TestHelpModalHostContent(t *testing.T) {
	m := tviewui.NewHelpModal("host")
	text := m.Text()
	for _, want := range []string{"Tab", "Ctrl+B", "/", "?", "Ctrl+Q", "Enter", "j", "k"} {
		if !strings.Contains(text, want) {
			t.Errorf("help missing %q:\n%s", want, text)
		}
	}
}

func TestHelpModalTerminalContent(t *testing.T) {
	m := tviewui.NewHelpModal("terminal")
	text := m.Text()
	for _, want := range []string{"Ctrl+\\", "Ctrl+D", "Copy"} {
		if !strings.Contains(text, want) {
			t.Errorf("terminal help missing %q:\n%s", want, text)
		}
	}
}

func TestHelpModalClose(t *testing.T) {
	m := tviewui.NewHelpModal("host")
	closed := false
	m.SetOnClose(func() { closed = true })
	m.TriggerClose()
	if !closed {
		t.Error("expected close callback")
	}
}