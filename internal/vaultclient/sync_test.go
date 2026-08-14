package vaultclient

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// fakeCiphersServer returns a mock VaultWarden server that responds to
// /api/ciphers with {"data":[...]}.
func fakeCiphersServer(t *testing.T, ciphersPayload string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/ciphers", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"continuationToken":null,"data":` + ciphersPayload + `}`))
	})
	return httptest.NewServer(mux)
}

func TestSyncReturnsCiphersFromCiphersEndpoint(t *testing.T) {
	// Sync always uses /api/ciphers (not /api/sync) because VaultWarden's
	// /api/sync does not reliably include the sshKey object on SSH-Key
	// ciphers. /api/ciphers is the same endpoint `bw list items` uses.
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
	srv := fakeCiphersServer(t, ciphersJSON)
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
	// Cipher + CustomField structs have correct json tags.
	ciphersJSON := `[
		{
			"id": "c1",
			"name": "2.enc==",
			"type": 5,
			"sshKey": {
				"privateKey": "2.pk==",
				"publicKey": "2.pub==",
				"keyFingerprint": "2.fp==",
				"passphrase": "2.pp=="
			},
			"fields": [{"name":"2.n==","value":"2.v==","type":0}]
		}
	]`
	srv := fakeCiphersServer(t, ciphersJSON)
	defer srv.Close()

	c := New(srv.URL)
	sess := &Session{AccessToken: "tok"}
	sr, err := c.Sync(sess)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if len(sr.Ciphers) != 1 {
		t.Fatalf("expected 1 cipher, got %d", len(sr.Ciphers))
	}
	if sr.Ciphers[0].ID != "c1" {
		t.Errorf("ID = %q, want %q", sr.Ciphers[0].ID, "c1")
	}
	if sr.Ciphers[0].SshKey == nil || sr.Ciphers[0].SshKey.PrivateKey != "2.pk==" {
		t.Errorf("SshKey.PrivateKey not parsed correctly")
	}
	if sr.Ciphers[0].SshKey.Passphrase != "2.pp==" {
		t.Errorf("SshKey.Passphrase not parsed correctly")
	}
}

func TestSyncEmptyVaultReturnsNoCiphers(t *testing.T) {
	srv := fakeCiphersServer(t, `[]`)
	defer srv.Close()

	c := New(srv.URL)
	sess := &Session{AccessToken: "tok"}
	sr, err := c.Sync(sess)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if len(sr.Ciphers) != 0 {
		t.Errorf("expected 0 ciphers, got %d", len(sr.Ciphers))
	}
}

func TestSyncOnlyReturnsSshKeyItemsWithPrivateKey(t *testing.T) {
	// Items without sshKey or with empty privateKey should be present in
	// the raw cipher list (filtering happens in vaultadapter, not here).
	// Sync returns ALL ciphers; vaultadapter.Items() filters for SSH keys.
	ciphersJSON := `[
		{"id":"login-1","name":"2.enc==","type":1,"login":{"username":"2.u=="}},
		{"id":"note-1","name":"2.enc==","type":2},
		{"id":"ssh-1","name":"2.enc==","type":5,"sshKey":{"privateKey":"2.pk==","publicKey":"2.pub==","keyFingerprint":"2.fp=="}
		}
	]`
	srv := fakeCiphersServer(t, ciphersJSON)
	defer srv.Close()

	c := New(srv.URL)
	sess := &Session{AccessToken: "tok"}
	sr, err := c.Sync(sess)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if len(sr.Ciphers) != 3 {
		t.Fatalf("expected 3 ciphers (all types), got %d", len(sr.Ciphers))
	}
	// Only the SSH key cipher has SshKey populated
	sshCount := 0
	for _, ci := range sr.Ciphers {
		if ci.SshKey != nil && ci.SshKey.PrivateKey != "" {
			sshCount++
		}
	}
	if sshCount != 1 {
		t.Errorf("expected 1 cipher with sshKey, got %d", sshCount)
	}
}

// TestSyncParsesLoginObject: a Type-1 login cipher's username+password are
// populated from the camelCase "login" object.
func TestSyncParsesLoginObject(t *testing.T) {
	ciphersJSON := `[
		{
			"id": "login-1",
			"name": "2.enc==",
			"type": 1,
			"login": {"username": "2.u==", "password": "2.p=="}
		}
	]`
	srv := fakeCiphersServer(t, ciphersJSON)
	defer srv.Close()

	c := New(srv.URL)
	sess := &Session{AccessToken: "tok"}
	sr, err := c.Sync(sess)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if len(sr.Ciphers) != 1 {
		t.Fatalf("expected 1 cipher, got %d", len(sr.Ciphers))
	}
	ci := sr.Ciphers[0]
	if ci.Login == nil {
		t.Fatal("Login is nil")
	}
	if ci.Login.Username != "2.u==" {
		t.Errorf("Username = %q, want 2.u==", ci.Login.Username)
	}
	if ci.Login.Password != "2.p==" {
		t.Errorf("Password = %q, want 2.p==", ci.Login.Password)
	}
}
