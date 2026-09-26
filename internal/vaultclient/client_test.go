package vaultclient

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClientRefreshToken(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/identity/connect/token", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm: %v", err)
		}
		if got := r.FormValue("grant_type"); got != "refresh_token" {
			t.Errorf("grant_type = %q, want refresh_token", got)
		}
		if got := r.FormValue("refresh_token"); got != "existing-refresh-token" {
			t.Errorf("refresh_token = %q, want existing-refresh-token", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"access_token":"new-acc-token","refresh_token":"new-ref-token","expires_in":3600,"token_type":"Bearer"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New(srv.URL)
	sess, err := c.RefreshToken("existing-refresh-token")
	if err != nil {
		t.Fatalf("RefreshToken failed: %v", err)
	}
	if sess.AccessToken != "new-acc-token" {
		t.Errorf("got AccessToken = %q, want new-acc-token", sess.AccessToken)
	}
	if sess.RefreshToken != "new-ref-token" {
		t.Errorf("got RefreshToken = %q, want new-ref-token", sess.RefreshToken)
	}
}

func TestNewHTTPClientHasTimeout(t *testing.T) {
	c := New("https://vault.example")
	if c.HTTP == nil {
		t.Fatal("HTTP client is nil")
	}
	if c.HTTP == http.DefaultClient {
		t.Fatal("New uses http.DefaultClient, which has no timeout")
	}
	if c.HTTP.Timeout < 15*time.Second {
		t.Fatalf("HTTP timeout = %s, want at least 15s", c.HTTP.Timeout)
	}
}

func TestLoginErrorHidesRawBody(t *testing.T) {
	raw := `{"error":"invalid_grant","error_description":"Username or password is incorrect. Try again","ExceptionMessage":"stack trace secret"}`
	mux := http.NewServeMux()
	mux.HandleFunc("/identity/accounts/prelogin", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"Kdf":0,"KdfIterations":600000}`))
	})
	mux.HandleFunc("/identity/connect/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(raw))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	_, err := New(srv.URL).Login("user@example.com", "wrong-password")
	if err == nil {
		t.Fatal("expected login error")
	}
	msg := err.Error()
	if strings.Contains(msg, "invalid_grant") || strings.Contains(msg, "ExceptionMessage") || strings.Contains(msg, raw) {
		t.Fatalf("login error leaked the raw response: %s", msg)
	}
	if !strings.Contains(msg, "Wrong master password") {
		t.Fatalf("login error = %q, want a wrong-password message", msg)
	}
}

func TestLoginErrorUnreachable(t *testing.T) {
	c := New("http://127.0.0.1:1")
	c.HTTP.Timeout = 200 * time.Millisecond
	_, err := c.Login("user@example.com", "password")
	if err == nil {
		t.Fatal("expected login error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "Could not reach the vault") {
		t.Fatalf("login error = %q, want an unreachable-vault message", msg)
	}
}
