package tviewui_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ac-kurniawan/wardenssh/internal/config"
	"github.com/ac-kurniawan/wardenssh/internal/hosts"
	"github.com/ac-kurniawan/wardenssh/internal/tviewui"
	"github.com/ac-kurniawan/wardenssh/internal/vaultadapter"
	"github.com/ac-kurniawan/wardenssh/internal/vaultclient"
)

func newAddModal(t *testing.T) *tviewui.AddVaultModal {
	t.Helper()
	return tviewui.NewAddVaultModal(
		[]config.Vault{{Name: "existing", Server: "https://vw.example.com", Email: "a@x"}},
		config.CustomFields{},
		hosts.NewList(nil),
	)
}

// TestAddVaultModalInitialState: the form collects the vault identity
// (name/server/email) plus the master password — unlike setup, these are
// editable inputs.
func TestAddVaultModalInitialState(t *testing.T) {
	m := newAddModal(t)
	if m.Name() != "" {
		t.Errorf("Name() = %q, want empty", m.Name())
	}
	if m.Server() != "https://" {
		t.Errorf("Server() = %q, want https:// placeholder", m.Server())
	}
	if m.Error() != "" || m.IsDone() {
		t.Errorf("initial Error/IsDone = %q/%v, want clean", m.Error(), m.IsDone())
	}
}

// TestAddVaultModalValidation: every rejected field surfaces an error without
// starting a login (IsDone stays false, no busy state).
func TestAddVaultModalValidation(t *testing.T) {
	cases := []struct {
		name, server, email, pass, want string
	}{
		{"", "https://vw", "a@x", "pw", "empty name"},
		{"bad\x01name", "https://vw", "a@x", "pw", "control char in name"},
		{"existing", "https://vw", "a@x", "pw", "duplicate of configured vault"},
		{"new", "notaurl", "a@x", "pw", "non-http server"},
		{"new", "https://vw", "no-at-sign", "pw", "email without @"},
		{"new", "https://vw", "a@x", "", "empty master password"},
	}
	for _, c := range cases {
		m := newAddModal(t)
		m.SetName(c.name)
		m.SetServer(c.server)
		m.SetEmail(c.email)
		m.SetPassword(c.pass)
		m.Submit()
		if m.Error() == "" {
			t.Errorf("Submit(%s): no error shown", c.want)
		}
		if m.IsDone() {
			t.Errorf("Submit(%s): done despite invalid input", c.want)
		}
	}
}

// TestAddVaultModalCancel: Esc/Cancel fires onCancel, never onAdded.
func TestAddVaultModalCancel(t *testing.T) {
	m := newAddModal(t)
	cancelled := false
	m.SetOnCancel(func() { cancelled = true })
	m.SetOnAdded(func(*vaultadapter.Source, config.Vault) {})
	m.Cancel()
	if !cancelled {
		t.Fatal("onCancel not fired")
	}
}

// vaultServer returns an httptest VaultWarden whose login succeeds for the
// given email/password and whose sync returns one (empty) cipher list.
func vaultServer(t *testing.T, email, pass string) *httptest.Server {
	t.Helper()
	ak, err := vaultclient.DeriveAccountKeys(email, pass, 1000)
	if err != nil {
		t.Fatalf("DeriveAccountKeys: %v", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/identity/accounts/prelogin", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"kdf":0,"kdfIterations":1000}`))
	})
	mux.HandleFunc("/identity/connect/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]string{
			"access_token":  "mock-at",
			"refresh_token": "mock-rt",
			"Key":           ak.ProtectedKey,
		}
		json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/sync", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"Ciphers":[],"Profile":{},"Folders":[]}`))
	})
	mux.HandleFunc("/api/ciphers", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":[]}`))
	})
	return httptest.NewServer(mux)
}

// TestAddVaultModalSuccess: after a successful login+sync the modal stores the
// refresh token in the keyring, merges the new vault's hosts into the host
// list, and hands the caller the authenticated source plus the config entry
// (name trimmed).
func TestAddVaultModalSuccess(t *testing.T) {
	srv := vaultServer(t, "user@example.com", "pass")
	defer srv.Close()

	var savedVault, savedToken string
	tviewui.SetKeyringSetRefreshTokenForTest(func(vName, token string) error {
		savedVault, savedToken = vName, token
		return nil
	})
	defer tviewui.ResetKeyringSetRefreshTokenForTest()

	hl := hosts.NewList(nil)
	m2 := tviewui.NewAddVaultModal(
		[]config.Vault{{Name: "existing"}},
		config.CustomFields{},
		hl,
	)

	added := make(chan config.Vault, 1)
	m2.SetOnAdded(func(_ *vaultadapter.Source, v config.Vault) { added <- v })

	m2.SetName("  work  ") // trimmed before use
	m2.SetServer(srv.URL)
	m2.SetEmail("user@example.com")
	m2.SetPassword("pass")
	m2.Submit()

	select {
	case v := <-added:
		if v.Name != "work" {
			t.Errorf("added vault name = %q, want trimmed %q", v.Name, "work")
		}
		if v.Server != srv.URL || v.Email != "user@example.com" {
			t.Errorf("added vault = %+v", v)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("add did not complete, err: %s", m2.Error())
	}

	if savedVault != "work" || savedToken != "mock-rt" {
		t.Errorf("keyring saved %q/%q, want work/mock-rt", savedVault, savedToken)
	}
	if !m2.IsDone() {
		t.Error("IsDone = false after successful add")
	}
}

// TestAddVaultModalLoginFailureKeepsForm: a wrong master password shows the
// error and keeps the typed identity fields so the user can retry — the vault
// is not added and no token is stored.
func TestAddVaultModalLoginFailureKeepsForm(t *testing.T) {
	// Server derives keys for a different password, so login decrypt fails or
	// returns wrong key → login error path.
	srv := vaultServer(t, "user@example.com", "different-password")
	defer srv.Close()

	stored := false
	tviewui.SetKeyringSetRefreshTokenForTest(func(string, string) error {
		stored = true
		return nil
	})
	defer tviewui.ResetKeyringSetRefreshTokenForTest()

	m := newAddModal(t)
	added := false
	m.SetOnAdded(func(_ *vaultadapter.Source, _ config.Vault) { added = true })
	m.SetName("work")
	m.SetServer(srv.URL)
	m.SetEmail("user@example.com")
	m.SetPassword("pass")
	m.Submit()

	deadline := time.Now().Add(3 * time.Second)
	for m.Error() == "" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if m.Error() == "" {
		t.Fatal("no error shown after failed login")
	}
	if m.IsDone() || added {
		t.Error("vault added despite failed login")
	}
	if stored {
		t.Error("refresh token stored despite failed login")
	}
	if m.Name() != "work" || m.Email() != "user@example.com" {
		t.Errorf("form cleared on failure: %q/%q", m.Name(), m.Email())
	}
}

// TestAddVaultModalSyncFailureAborts: a login that succeeds but a sync that
// fails must abort the add entirely — nothing added, nothing stored, error
// shown, form kept for retry.
func TestAddVaultModalSyncFailureAborts(t *testing.T) {
	ak, err := vaultclient.DeriveAccountKeys("user@example.com", "pass", 1000)
	if err != nil {
		t.Fatalf("DeriveAccountKeys: %v", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/identity/accounts/prelogin", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"kdf":0,"kdfIterations":1000}`))
	})
	mux.HandleFunc("/identity/connect/token", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{
			"access_token":  "at",
			"refresh_token": "rt",
			"Key":           ak.ProtectedKey,
		})
	})
	// Both sync endpoints fail.
	mux.HandleFunc("/api/sync", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	mux.HandleFunc("/api/ciphers", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	stored := false
	tviewui.SetKeyringSetRefreshTokenForTest(func(string, string) error {
		stored = true
		return nil
	})
	defer tviewui.ResetKeyringSetRefreshTokenForTest()

	m := newAddModal(t)
	added := false
	m.SetOnAdded(func(_ *vaultadapter.Source, _ config.Vault) { added = true })
	m.SetName("work")
	m.SetServer(srv.URL)
	m.SetEmail("user@example.com")
	m.SetPassword("pass")
	m.Submit()

	deadline := time.Now().Add(3 * time.Second)
	for m.Error() == "" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if m.Error() == "" {
		t.Fatal("no error shown after failed sync")
	}
	if m.IsDone() || added {
		t.Error("vault added despite failed sync")
	}
	if stored {
		t.Error("refresh token stored despite failed sync")
	}
}
