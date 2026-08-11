package vaultclient

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeVaultServer returns a mock VaultWarden server that responds to
// /api/sync with an empty ciphers array (matching real VW behavior where
// sync may return no ciphers) and /api/ciphers with the actual item list
// wrapped as {"data":[...]}.
func fakeVaultServer(t *testing.T, ciphersPayload string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/sync", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"object":"sync","profile":{"id":"u1","email":"test@example.com"},"ciphers":[]}`))
	})
	mux.HandleFunc("/api/ciphers", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"continuationToken":null,"data":` + ciphersPayload + `}`))
	})
	return httptest.NewServer(mux)
}

func TestSyncFallsBackToCiphersEndpoint(t *testing.T) {
	// VaultWarden's /api/sync may return an empty ciphers array even when
	// items exist. In that case Sync must fall back to /api/ciphers (which
	// returns {"data":[...]}) so the caller sees the items.
	ciphersJSON := `[
		{
			"id": "abc-123",
			"name": "2.encrypted==",
			"type": 5,
			"sshKey": {
				"privateKey": "2.encrypted==",
				"publicKey": "2.encrypted==",
				"keyFingerprint": "2.encrypted=="
			},
			"fields": [
				{"name": "2.enc==", "value": "2.enc==", "type": 0}
			]
		}
	]`
	srv := fakeVaultServer(t, ciphersJSON)
	defer srv.Close()

	c := New(srv.URL)
	sess := &Session{AccessToken: "test-token"}
	sr, err := c.Sync(sess)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if len(sr.Ciphers) != 1 {
		t.Fatalf("expected 1 cipher, got %d", len(sr.Ciphers))
	}
	ci := sr.Ciphers[0]
	if ci.ID != "abc-123" {
		t.Errorf("ID = %q, want %q", ci.ID, "abc-123")
	}
	if ci.Type != 5 {
		t.Errorf("Type = %d, want 5", ci.Type)
	}
	if ci.SshKey == nil {
		t.Fatal("SshKey is nil")
	}
	if ci.SshKey.PrivateKey != "2.encrypted==" {
		t.Errorf("PrivateKey = %q", ci.SshKey.PrivateKey)
	}
	if len(ci.Fields) != 1 {
		t.Fatalf("expected 1 field, got %d", len(ci.Fields))
	}
	if ci.Fields[0].Name != "2.enc==" {
		t.Errorf("Field Name = %q", ci.Fields[0].Name)
	}
}

func TestSyncParsesCamelCaseKeys(t *testing.T) {
	// VaultWarden uses camelCase JSON keys (not PascalCase). Verify that
	// SyncResponse + Cipher structs have correct json tags.
	syncPayload := `{
		"object": "sync",
		"profile": {"id": "u1", "email": "test@example.com"},
		"ciphers": [
			{
				"id": "c1",
				"name": "2.enc==",
				"type": 5,
				"sshKey": {
					"privateKey": "2.pk==",
					"publicKey": "2.pub==",
					"keyFingerprint": "2.fp=="
				},
				"fields": [{"name":"2.n==","value":"2.v==","type":0}]
			}
		]
	}`
	mux := http.NewServeMux()
	mux.HandleFunc("/api/sync", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(syncPayload))
	})
	mux.HandleFunc("/api/ciphers", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":[]}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New(srv.URL)
	sess := &Session{AccessToken: "tok"}
	sr, err := c.Sync(sess)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if len(sr.Ciphers) != 1 {
		t.Fatalf("expected 1 cipher from /api/sync, got %d", len(sr.Ciphers))
	}
	if sr.Ciphers[0].ID != "c1" {
		t.Errorf("ID = %q, want %q", sr.Ciphers[0].ID, "c1")
	}
	if sr.Ciphers[0].SshKey == nil || sr.Ciphers[0].SshKey.PrivateKey != "2.pk==" {
		t.Errorf("SshKey.PrivateKey not parsed correctly")
	}
	if sr.Profile.ID != "u1" {
		t.Errorf("Profile.ID = %q, want %q", sr.Profile.ID, "u1")
	}
	if sr.Profile.Email != "test@example.com" {
		t.Errorf("Profile.Email = %q, want %q", sr.Profile.Email, "test@example.com")
	}
}

// suppress unused import warning for json if not currently used
var _ = json.Marshal
var _ = strings.TrimSpace
