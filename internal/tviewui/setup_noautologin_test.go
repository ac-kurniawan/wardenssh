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
)

// TestSetupModalDoesNotAutoLoginWithKeyringToken: even when a refresh token
// exists in the keyring, the setup modal must STILL require the master
// password. Auto-login is removed because a refresh token alone cannot derive
// the vault symmetric key (the master password is required every launch).
func TestSetupModalDoesNotAutoLoginWithKeyringToken(t *testing.T) {
	tviewui.SetKeyringGetRefreshTokenForTest(func(vName string) (string, error) {
		if vName == "myvault" {
			return "valid-ref-token", nil
		}
		return "", fmt.Errorf("no token")
	})
	defer tviewui.ResetKeyringGetRefreshTokenForTest()

	// Mock a working token + ciphers endpoint so auto-login WOULD succeed if
	// it were still wired up.
	mux := http.NewServeMux()
	mux.HandleFunc("/identity/connect/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"access_token":  "mock-at",
			"refresh_token": "valid-ref-token",
		})
	})
	mux.HandleFunc("/api/ciphers", func(w http.ResponseWriter, r *http.Request) {
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

	// Give any (removed) auto-login goroutine time to (not) run.
	time.Sleep(150 * time.Millisecond)

	if m.IsDone() {
		t.Fatal("expected setup modal to require the master password, but auto-login completed setup")
	}
	if !strings.Contains(m.CurrentPrompt(), "myvault") {
		t.Errorf("expected password prompt for myvault, got: %s", m.CurrentPrompt())
	}
}
