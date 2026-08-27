package tviewui_test

import (
	"strings"
	"testing"

	"github.com/ac-kurniawan/wardenssh/internal/tviewui"
)

// TestFooterHostModeAdvertisesCtrlDDelete: the host-mode footer must advertise
// Ctrl+D as the delete shortcut (revamp: Ctrl+D replaces the Delete key).
func TestFooterHostModeAdvertisesCtrlDDelete(t *testing.T) {
	f := tviewui.NewFooter() // defaults to host mode
	text := f.Text()
	if !strings.Contains(text, "Ctrl+D") {
		t.Errorf("host footer must advertise Ctrl+D: %q", text)
	}
	if !strings.Contains(text, "Delete") {
		t.Errorf("host footer must advertise Delete: %q", text)
	}
}

// TestFooterTerminalModeKeepsCtrlDDisconnect: the terminal-mode footer keeps
// Ctrl+D = Disconnect (unchanged — terminal pane behavior is not repurposed).
func TestFooterTerminalModeKeepsCtrlDDisconnect(t *testing.T) {
	f := tviewui.NewFooter()
	f.SetMode("terminal")
	text := f.Text()
	if !strings.Contains(text, "Ctrl+D") {
		t.Errorf("terminal footer must still advertise Ctrl+D: %q", text)
	}
	if !strings.Contains(text, "Disconnect") {
		t.Errorf("terminal footer must still advertise Disconnect: %q", text)
	}
}

// TestHelpModalHostAdvertisesCtrlDDelete: the host-mode help sheet must list
// Ctrl+D Delete (replacing the old [Delete] Delete connection entry).
func TestHelpModalHostAdvertisesCtrlDDelete(t *testing.T) {
	m := tviewui.NewHelpModal("host")
	text := m.Text()
	if !strings.Contains(text, "Ctrl+D") {
		t.Errorf("host help must advertise Ctrl+D: %s", text)
	}
	if !strings.Contains(text, "Delete host") {
		t.Errorf("host help must say 'Delete host': %s", text)
	}
	// The old standalone [Delete] entry is gone (Ctrl+D replaces it).
	if strings.Contains(text, "[Delete]") {
		t.Errorf("host help must not advertise the old [Delete] key: %s", text)
	}
}

// TestHelpModalTerminalKeepsCtrlDDisconnect: the terminal help sheet keeps
// Ctrl+D = Disconnect session (unchanged).
func TestHelpModalTerminalKeepsCtrlDDisconnect(t *testing.T) {
	m := tviewui.NewHelpModal("terminal")
	text := m.Text()
	if !strings.Contains(text, "Ctrl+D") {
		t.Errorf("terminal help must still advertise Ctrl+D: %s", text)
	}
	if !strings.Contains(text, "Disconnect") {
		t.Errorf("terminal help must still advertise Disconnect: %s", text)
	}
}
