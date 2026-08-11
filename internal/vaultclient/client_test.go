package vaultclient

import (
	"net/http"
	"net/http/httptest"
	"testing"
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
