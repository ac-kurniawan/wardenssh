package tviewui_test

import (
	"strings"
	"testing"

	"github.com/ac-kurniawan/wardenssh/internal/tviewui"
)

func TestFooterHostModeHints(t *testing.T) {
	f := tviewui.NewFooter()
	text := f.Text()
	for _, want := range []string{"Tab", "Ctrl+N", "Ctrl+R", "Enter", "quit"} {
		if !strings.Contains(text, want) {
			t.Errorf("host-mode footer %q missing %q", text, want)
		}
	}
}

func TestFooterSwitchesToTerminalMode(t *testing.T) {
	f := tviewui.NewFooter()
	f.SetMode("terminal")
	text := f.Text()
	for _, want := range []string{"Ctrl+B", "Ctrl+C", "Esc"} {
		if !strings.Contains(text, want) {
			t.Errorf("terminal-mode footer %q missing %q", text, want)
		}
	}
}

func TestAppFooterTracksFocus(t *testing.T) {
	app := tviewui.New(sampleHostList(), tviewui.Deps{}, nil)
	if !strings.Contains(app.FooterText(), "Tab") {
		t.Errorf("default footer = %q, want host-mode hints", app.FooterText())
	}
	app.FocusTerminal()
	if !strings.Contains(app.FooterText(), "Ctrl+B") {
		t.Errorf("after FocusTerminal footer = %q, want terminal-mode hints", app.FooterText())
	}
	app.FocusHostList()
	if !strings.Contains(app.FooterText(), "Tab") {
		t.Errorf("after FocusHostList footer = %q, want host-mode hints", app.FooterText())
	}
}
