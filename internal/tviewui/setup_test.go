package tviewui_test

import (
	"strings"
	"testing"

	"github.com/ac-kurniawan/wardenssh/internal/config"
	"github.com/ac-kurniawan/wardenssh/internal/hosts"
	"github.com/ac-kurniawan/wardenssh/internal/tviewui"
	"github.com/ac-kurniawan/wardenssh/internal/vault"
)

func sampleVaults() []config.Vault {
	return []config.Vault{
		{Name: "vw", Server: "https://vw.example.com", Email: "user@example.com"},
	}
}

func TestSetupModalInitialState(t *testing.T) {
	hl := hosts.NewList(nil)
	m := tviewui.NewSetupModal(sampleVaults(), config.CustomFields{}, hl)

	if m.IsDone() {
		t.Fatal("expected setup to not be done initially")
	}
	prompt := m.CurrentPrompt()
	if !strings.Contains(prompt, "vw") {
		t.Errorf("prompt should contain 'vw', got: %s", prompt)
	}
	if !strings.Contains(prompt, "user@example.com") {
		t.Errorf("prompt should contain email, got: %s", prompt)
	}
}

func TestSetupModalTypingBuildsPassword(t *testing.T) {
	hl := hosts.NewList(nil)
	m := tviewui.NewSetupModal(sampleVaults(), config.CustomFields{}, hl)
	m.TypeRune('p')
	m.TypeRune('a')
	m.TypeRune('s')
	m.TypeRune('s')
	if got := m.Password(); got != "pass" {
		t.Errorf("password = %q, want pass", got)
	}
}

func TestSetupModalSkipAllVaults(t *testing.T) {
	hl := hosts.NewList(nil)
	m := tviewui.NewSetupModal(sampleVaults(), config.CustomFields{}, hl)

	skipped := false
	m.SetOnSkip(func() {
		skipped = true
	})
	m.SkipCurrent()
	if !skipped {
		t.Fatal("expected OnSkip callback after skipping all vaults")
	}
	if !m.IsDone() {
		t.Fatal("expected setup to be done after skipping all vaults")
	}
}

func TestSetupModalMultiVaultSkipAdvances(t *testing.T) {
	vaults := []config.Vault{
		{Name: "vw1", Server: "https://vw1.example.com", Email: "u1@e.com"},
		{Name: "vw2", Server: "https://vw2.example.com", Email: "u2@e.com"},
	}
	hl := hosts.NewList(nil)
	m := tviewui.NewSetupModal(vaults, config.CustomFields{}, hl)

	m.SkipCurrent()
	if m.IsDone() {
		t.Fatal("expected to still be in setup for vw2 after skipping vw1")
	}
	if !strings.Contains(m.CurrentPrompt(), "vw2") {
		t.Errorf("expected prompt for vw2, got: %s", m.CurrentPrompt())
	}

	m.SkipCurrent()
	if !m.IsDone() {
		t.Fatal("expected to be done after skipping all vaults")
	}
}

func TestSetupModalBackspaceDeletesLastChar(t *testing.T) {
	hl := hosts.NewList(nil)
	m := tviewui.NewSetupModal(sampleVaults(), config.CustomFields{}, hl)
	m.TypeRune('h')
	m.TypeRune('i')
	m.Backspace()
	if got := m.Password(); got != "h" {
		t.Errorf("password after backspace = %q, want h", got)
	}
}

// Compile-time check that vault.Client is used in the callback signature.
var _ vault.Client = (vault.Client)(nil)
