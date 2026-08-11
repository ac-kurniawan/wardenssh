package tviewui_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ac-kurniawan/wardenssh/internal/config"
	"github.com/ac-kurniawan/wardenssh/internal/hosts"
	"github.com/ac-kurniawan/wardenssh/internal/tviewui"
	"github.com/ac-kurniawan/wardenssh/internal/vault"
	"github.com/ac-kurniawan/wardenssh/internal/vaultclient"
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

func TestSetupModalSavesRefreshTokenToKeyringOnLogin(t *testing.T) {
	var savedVaultName, savedRefreshToken string
	tviewui.SetKeyringSetRefreshTokenForTest(func(vName, token string) error {
		savedVaultName = vName
		savedRefreshToken = token
		return nil
	})
	defer tviewui.ResetKeyringSetRefreshTokenForTest()

	ak, err := vaultclient.DeriveAccountKeys("user@example.com", "pass", 1000)
	if err != nil {
		t.Fatalf("DeriveAccountKeys: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/identity/accounts/prelogin", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"kdf":0,"kdfIterations":1000}`))
	})
	mux.HandleFunc("/identity/connect/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]string{
			"access_token":  "mock-at",
			"refresh_token": "mock-rt-456",
			"Key":           ak.ProtectedKey,
		}
		json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/ciphers", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":[]}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	vaults := []config.Vault{
		{Name: "myvault", Server: srv.URL, Email: "user@example.com"},
	}
	hl := hosts.NewList(nil)
	m := tviewui.NewSetupModal(vaults, config.CustomFields{}, hl)

	doneCh := make(chan struct{})
	m.SetOnComplete(func(vc vault.Client) {
		close(doneCh)
	})

	m.TypeRune('p')
	m.TypeRune('a')
	m.TypeRune('s')
	m.TypeRune('s')
	m.Submit()

	select {
	case <-doneCh:
	case <-time.After(3 * time.Second):
		t.Fatalf("setup did not complete in time, last err: %s", m.Error())
	}

	if savedVaultName != "myvault" {
		t.Errorf("savedVaultName = %q, want myvault", savedVaultName)
	}
	if savedRefreshToken != "mock-rt-456" {
		t.Errorf("savedRefreshToken = %q, want mock-rt-456", savedRefreshToken)
	}
}

// Compile-time check that vault.Client is used in the callback signature.
var _ vault.Client = (vault.Client)(nil)

func TestSetupModalAutoLoginWithKeyring(t *testing.T) {
	tviewui.SetKeyringGetRefreshTokenForTest(func(vName string) (string, error) {
		if vName == "myvault" {
			return "valid-ref-token", nil
		}
		return "", fmt.Errorf("no token")
	})
	defer tviewui.ResetKeyringGetRefreshTokenForTest()

	mux := http.NewServeMux()
	mux.HandleFunc("/identity/connect/token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm: %v", err)
		}
		if r.FormValue("grant_type") != "refresh_token" {
			t.Errorf("grant_type = %q, want refresh_token", r.FormValue("grant_type"))
		}
		if r.FormValue("refresh_token") != "valid-ref-token" {
			t.Errorf("refresh_token = %q, want valid-ref-token", r.FormValue("refresh_token"))
		}
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]string{
			"access_token":  "mock-at",
			"refresh_token": "valid-ref-token",
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/ciphers", func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer mock-at" {
			t.Errorf("Authorization = %q, want Bearer mock-at", auth)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	vaults := []config.Vault{
		{Name: "myvault", Server: srv.URL, Email: "user@example.com"},
	}
	hl := hosts.NewList(nil)
	m := tviewui.NewSetupModal(vaults, config.CustomFields{}, hl)

	doneCh := make(chan struct{})
	m.SetOnComplete(func(vc vault.Client) {
		close(doneCh)
	})

	select {
	case <-doneCh:
	case <-time.After(3 * time.Second):
		t.Fatalf("setup auto-login did not complete in time, last err: %s", m.Error())
	}

	if !m.IsDone() {
		t.Error("expected modal to be done after auto-login")
	}
}

func TestSetupModalAutoLoginFailureFallsBackToPasswordPrompt(t *testing.T) {
	tviewui.SetKeyringGetRefreshTokenForTest(func(vName string) (string, error) {
		return "expired-token", nil
	})
	defer tviewui.ResetKeyringGetRefreshTokenForTest()

	mux := http.NewServeMux()
	mux.HandleFunc("/identity/connect/token", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	vaults := []config.Vault{
		{Name: "myvault", Server: srv.URL, Email: "user@example.com"},
	}
	hl := hosts.NewList(nil)
	m := tviewui.NewSetupModal(vaults, config.CustomFields{}, hl)

	time.Sleep(100 * time.Millisecond)

	if m.IsDone() {
		t.Fatal("expected setup not to be done when auto-login fails")
	}

	if !strings.Contains(m.CurrentPrompt(), "myvault") {
		t.Errorf("expected prompt for myvault, got: %s", m.CurrentPrompt())
	}
}

