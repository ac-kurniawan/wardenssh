package tviewui_test

import (
	"strings"
	"testing"

	"github.com/ac-kurniawan/wardenssh/internal/tviewui"
)

func TestFooterHostModeHints(t *testing.T) {
	f := tviewui.NewFooter()
	text := f.Text()
	for _, want := range []string{"Select", "Connect", "Terminal", "Filter", "Scopes", "Quit"} {
		if !strings.Contains(text, want) {
			t.Errorf("host footer %q missing %q", text, want)
		}
	}
}

func TestFooterUsesKeyTagColor(t *testing.T) {
	f := tviewui.NewFooter()
	if !strings.Contains(f.RawText(), "#A855F7") {
		t.Errorf("footer must use violet key tags: %q", f.RawText())
	}
}

func TestFooterSwitchesToTerminalMode(t *testing.T) {
	f := tviewui.NewFooter()
	f.SetMode("terminal")
	text := f.Text()
	for _, want := range []string{"Ctrl+\\", "Ctrl+D", "Copy"} {
		if !strings.Contains(text, want) {
			t.Errorf("terminal footer %q missing %q", text, want)
		}
	}
}

func TestAppFooterTracksFocus(t *testing.T) {
	app := tviewui.New(sampleHostList(), tviewui.Deps{}, nil)
	if !strings.Contains(app.FooterText(), "Select") {
		t.Errorf("default footer = %q, want host-mode hints", app.FooterText())
	}
	app.FocusTerminal()
	if !strings.Contains(app.FooterText(), "Ctrl+\\") {
		t.Errorf("after FocusTerminal footer = %q, want terminal-mode hints", app.FooterText())
	}
	app.FocusHostList()
	if !strings.Contains(app.FooterText(), "Select") {
		t.Errorf("after FocusHostList footer = %q, want host-mode hints", app.FooterText())
	}
}