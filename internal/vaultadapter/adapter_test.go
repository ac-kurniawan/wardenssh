package vaultadapter_test

import (
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ac-kurniawan/wardenssh/internal/config"
	"github.com/ac-kurniawan/wardenssh/internal/vaultadapter"
	"github.com/ac-kurniawan/wardenssh/internal/vaultclient"
	"github.com/ac-kurniawan/wardenssh/internal/vaultcrypto"
)

// fakeSession builds a vaultclient.Session with a random symmetric key for
// encrypting/decrypting test data (no real vault needed).
func fakeSession(t *testing.T) *vaultclient.Session {
	t.Helper()
	symEnc := make([]byte, 32)
	symMac := make([]byte, 32)
	_, _ = rand.Read(symEnc)
	_, _ = rand.Read(symMac)
	return &vaultclient.Session{SymEnc: symEnc, SymMac: symMac}
}

// enc encrypts a plaintext string under the session's symmetric key.
func enc(t *testing.T, sess *vaultclient.Session, plain string) string {
	t.Helper()
	ct, err := vaultcrypto.Encrypt(sess.SymEnc, sess.SymMac, []byte(plain))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	return ct
}

// TestSourceItemsDecryptsFieldsAndFiltersByHost: the adapter decrypts item
// names + custom fields, filters by populated host (Q32/B), and leaves the
// private key encrypted for lazy decrypt.
func TestSourceItemsDecryptsFieldsAndFiltersByHost(t *testing.T) {
	sess := fakeSession(t)
	cf := config.Default().CustomFields

	ciphers := []vaultclient.Cipher{
		{
			ID:   "1",
			Name: enc(t, sess, "prod-db-01"),
			SshKey: &vaultclient.SshKey{PrivateKey: enc(t, sess, "PRIVATE-KEY-BYTES-1")},
			Fields: []vaultclient.CustomField{
				{Name: enc(t, sess, "host"), Value: enc(t, sess, "10.0.0.5"), Type: 0},
				{Name: enc(t, sess, "user"), Value: enc(t, sess, "admin"), Type: 0},
			},
		},
		{
			ID:   "2",
			Name: enc(t, sess, "no-host-item"),
			SshKey: &vaultclient.SshKey{PrivateKey: enc(t, sess, "PRIVATE-KEY-BYTES-2")},
			Fields: []vaultclient.CustomField{
				{Name: enc(t, sess, "host"), Value: "", Type: 0}, // empty host -> excluded
			},
		},
	}

	src := vaultadapter.NewSource("vw:personal", sess, ciphers, cf)
	items, err := src.Items()
	if err != nil {
		t.Fatalf("Items: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1 (no-host excluded by Q32/B)", len(items))
	}
	if items[0].Name != "prod-db-01" {
		t.Errorf("Name = %q, want prod-db-01", items[0].Name)
	}
	if items[0].HostName != "10.0.0.5" {
		t.Errorf("HostName = %q, want 10.0.0.5", items[0].HostName)
	}
	if items[0].User != "admin" {
		t.Errorf("User = %q, want admin", items[0].User)
	}
	// Private key must remain encrypted (lazy decrypt, Q8/C).
	if items[0].EncPrivateKey == "PRIVATE-KEY-BYTES-1" {
		t.Error("EncPrivateKey is plaintext — should be the encrypted form")
	}
}

// TestSourceDecryptPrivateKey: lazy decrypt returns the original plaintext key.
func TestSourceDecryptPrivateKey(t *testing.T) {
	sess := fakeSession(t)
	cf := config.Default().CustomFields
	encPriv := enc(t, sess, "MY-SECRET-ED25519-KEY")

	ciphers := []vaultclient.Cipher{
		{
			ID:   "1",
			Name: enc(t, sess, "test-host"),
			SshKey: &vaultclient.SshKey{PrivateKey: encPriv},
			Fields: []vaultclient.CustomField{
				{Name: enc(t, sess, "host"), Value: enc(t, sess, "1.2.3.4"), Type: 0},
			},
		},
	}
	src := vaultadapter.NewSource("vw:personal", sess, ciphers, cf)
	items, _ := src.Items()

	decrypted, err := src.DecryptPrivateKey(items[0], "")
	if err != nil {
		t.Fatalf("DecryptPrivateKey: %v", err)
	}
	if string(decrypted) != "MY-SECRET-ED25519-KEY" {
		t.Errorf("decrypted = %q, want MY-SECRET-ED25519-KEY", decrypted)
	}
}

// TestSourceRemoveCipherPurgesCache: after a permanent delete, RemoveCipher
// drops the cipher from the source's cached list so the deleted item can never
// resurface (e.g. if a later sync fails and the cache is re-read).
func TestSourceRemoveCipherPurgesCache(t *testing.T) {
	sess := fakeSession(t)
	cf := config.Default().CustomFields

	ciphers := []vaultclient.Cipher{
		{
			ID:   "1",
			Name: enc(t, sess, "keep-me"),
			SshKey: &vaultclient.SshKey{PrivateKey: enc(t, sess, "KEY-1")},
			Fields: []vaultclient.CustomField{
				{Name: enc(t, sess, "host"), Value: enc(t, sess, "10.0.0.1"), Type: 0},
			},
		},
		{
			ID:   "2",
			Name: enc(t, sess, "delete-me"),
			SshKey: &vaultclient.SshKey{PrivateKey: enc(t, sess, "KEY-2")},
			Fields: []vaultclient.CustomField{
				{Name: enc(t, sess, "host"), Value: enc(t, sess, "10.0.0.2"), Type: 0},
			},
		},
	}
	src := vaultadapter.NewSource("vw:personal", sess, ciphers, cf)

	before, _ := src.Items()
	if len(before) != 2 {
		t.Fatalf("before RemoveCipher: got %d items, want 2", len(before))
	}

	src.RemoveCipher("2")

	after, err := src.Items()
	if err != nil {
		t.Fatalf("Items after RemoveCipher: %v", err)
	}
	if len(after) != 1 {
		t.Fatalf("after RemoveCipher: got %d items, want 1", len(after))
	}
	if after[0].ID != "1" {
		t.Errorf("surviving item ID = %q, want 1", after[0].ID)
	}

	// Removing an unknown ID is a no-op, not an error.
	src.RemoveCipher("does-not-exist")
	still, _ := src.Items()
	if len(still) != 1 {
		t.Fatalf("removing unknown id changed the cache: got %d items", len(still))
	}
}

// TestClientMultiSource: the aggregate client exposes multiple sources by name.
func TestClientMultiSource(t *testing.T) {
	sess := fakeSession(t)
	cf := config.Default().CustomFields
	s1 := vaultadapter.NewSource("vw:personal", sess, nil, cf)
	s2 := vaultadapter.NewSource("vw:work", sess, nil, cf)
	c := vaultadapter.NewClient(s1, s2)
	if len(c.Sources()) != 2 {
		t.Fatalf("got %d sources, want 2", len(c.Sources()))
	}
	if c.Sources()[0].Name() != "vw:personal" || c.Sources()[1].Name() != "vw:work" {
		t.Errorf("source names = %q/%q", c.Sources()[0].Name(), c.Sources()[1].Name())
	}
}

// TestSourceSyncUpdatesCiphers: Sync re-fetches ciphers from vaultclient.Client
// and updates the source's cached ciphers so Items() reflects server state.
func TestSourceSyncUpdatesCiphers(t *testing.T) {
	sess := fakeSession(t)
	sess.AccessToken = "test-token"
	cf := config.Default().CustomFields

	ciphers1 := []vaultclient.Cipher{
		{
			ID:   "1",
			Name: enc(t, sess, "host-1"),
			SshKey: &vaultclient.SshKey{PrivateKey: enc(t, sess, "KEY-1")},
			Fields: []vaultclient.CustomField{
				{Name: enc(t, sess, "host"), Value: enc(t, sess, "10.0.0.1"), Type: 0},
			},
		},
	}
	src := vaultadapter.NewSource("vw:personal", sess, ciphers1, cf)

	itemsBefore, _ := src.Items()
	if len(itemsBefore) != 1 {
		t.Fatalf("before sync: got %d items, want 1", len(itemsBefore))
	}

	ciphersJSON := `{"data":[
		{
			"id": "1",
			"name": "` + enc(t, sess, "host-1") + `",
			"type": 5,
			"sshKey": {"privateKey": "` + enc(t, sess, "KEY-1") + `"},
			"fields": [{"name": "` + enc(t, sess, "host") + `", "value": "` + enc(t, sess, "10.0.0.1") + `", "type": 0}]
		},
		{
			"id": "2",
			"name": "` + enc(t, sess, "host-2") + `",
			"type": 5,
			"sshKey": {"privateKey": "` + enc(t, sess, "KEY-2") + `"},
			"fields": [{"name": "` + enc(t, sess, "host") + `", "value": "` + enc(t, sess, "10.0.0.2") + `", "type": 0}]
		}
	]}`

	mux := http.NewServeMux()
	mux.HandleFunc("/api/ciphers", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(ciphersJSON))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	vc := vaultclient.New(srv.URL)
	if err := src.Sync(vc); err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	itemsAfter, err := src.Items()
	if err != nil {
		t.Fatalf("Items after sync: %v", err)
	}
	if len(itemsAfter) != 2 {
		t.Fatalf("after sync: got %d items, want 2", len(itemsAfter))
	}
}

// TestClientSyncAllUpdatesSources: SyncAll syncs all underlying sources.
func TestClientSyncAllUpdatesSources(t *testing.T) {
	sess := fakeSession(t)
	sess.AccessToken = "test-token"
	cf := config.Default().CustomFields

	src1 := vaultadapter.NewSource("vw:personal", sess, nil, cf)
	src2 := vaultadapter.NewSource("vw:work", sess, nil, cf)
	client := vaultadapter.NewClient(src1, src2)

	ciphersJSON := `{"data":[
		{
			"id": "10",
			"name": "` + enc(t, sess, "host-10") + `",
			"type": 5,
			"sshKey": {"privateKey": "` + enc(t, sess, "KEY-10") + `"},
			"fields": [{"name": "` + enc(t, sess, "host") + `", "value": "` + enc(t, sess, "10.0.0.10") + `", "type": 0}]
		}
	]}`

	mux := http.NewServeMux()
	mux.HandleFunc("/api/ciphers", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(ciphersJSON))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	vc := vaultclient.New(srv.URL)
	if err := client.SyncAll(vc); err != nil {
		t.Fatalf("SyncAll failed: %v", err)
	}

	items1, _ := src1.Items()
	items2, _ := src2.Items()
	if len(items1) != 1 || len(items2) != 1 {
		t.Fatalf("expected 1 item per source after SyncAll, got %d and %d", len(items1), len(items2))
	}
}

// TestSourceItemsIncludesSSHLoginItems: login items are launchable only when
// they have a populated host AND a type==SSH (case-insensitive) custom field.
func TestSourceItemsIncludesSSHLoginItems(t *testing.T) {
	sess := fakeSession(t)
	cf := config.Default().CustomFields

	mkLogin := func(id, name, u, p string, fields []vaultclient.CustomField) vaultclient.Cipher {
		return vaultclient.Cipher{
			ID:   id,
			Name: enc(t, sess, name),
			Type: 1,
			Login: &vaultclient.Login{
				Username: enc(t, sess, u),
				Password: enc(t, sess, p),
			},
			Fields: fields,
		}
	}

	ciphers := []vaultclient.Cipher{
		// login with type==SSH + host -> included, user from login.username
		mkLogin("1", "prod-db", "admin", "s3cret", []vaultclient.CustomField{
			{Name: enc(t, sess, "host"), Value: enc(t, sess, "10.0.0.9"), Type: 0},
			{Name: enc(t, sess, "type"), Value: enc(t, sess, "SSH"), Type: 0},
		}),
		// login with type != SSH -> excluded
		mkLogin("2", "web-ui", "u", "p", []vaultclient.CustomField{
			{Name: enc(t, sess, "host"), Value: enc(t, sess, "web.internal"), Type: 0},
			{Name: enc(t, sess, "type"), Value: enc(t, sess, "HTTPS"), Type: 0},
		}),
		// login with no type field -> excluded
		mkLogin("3", "ftp-box", "u", "p", []vaultclient.CustomField{
			{Name: enc(t, sess, "host"), Value: enc(t, sess, "ftp.internal"), Type: 0},
		}),
		// login without host -> excluded even with type=ssh
		mkLogin("4", "no-host", "u", "p", []vaultclient.CustomField{
			{Name: enc(t, sess, "type"), Value: enc(t, sess, "ssh"), Type: 0},
		}),
		// login with type=ssh + host but EMPTY password -> excluded (raw empty
		// login.password — never encrypted, mirrors a vault with no password).
		{
			ID:   "5",
			Name: enc(t, sess, "empty-pass"),
			Type: 1,
			Login: &vaultclient.Login{
				Username: enc(t, sess, "u"),
				Password: "",
			},
			Fields: []vaultclient.CustomField{
				{Name: enc(t, sess, "host"), Value: enc(t, sess, "10.0.0.11"), Type: 0},
				{Name: enc(t, sess, "type"), Value: enc(t, sess, "ssh"), Type: 0},
			},
		},
	}

	src := vaultadapter.NewSource("vw:personal", sess, ciphers, cf)
	items, err := src.Items()
	if err != nil {
		t.Fatalf("Items: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1 (only prod-db qualifies)", len(items))
	}
	it := items[0]
	if it.Kind != "login" {
		t.Errorf("Kind = %q, want login", it.Kind)
	}
	if it.Name != "prod-db" || it.HostName != "10.0.0.9" || it.User != "admin" {
		t.Errorf("item = %+v", it)
	}
	// credentials stay encrypted (lazy decrypt at connect).
	if it.EncUsername == "admin" {
		t.Error("EncUsername is plaintext — should be the encrypted form")
	}
}

// TestSourceItemsSSHLoginTypeCaseInsensitive: type value matches case-insensitively.
func TestSourceItemsSSHLoginTypeCaseInsensitive(t *testing.T) {
	sess := fakeSession(t)
	cf := config.Default().CustomFields
	ci := vaultclient.Cipher{
		ID:    "1",
		Name:  enc(t, sess, "h"),
		Type:  1,
		Login: &vaultclient.Login{Username: enc(t, sess, "u"), Password: enc(t, sess, "p")},
		Fields: []vaultclient.CustomField{
			{Name: enc(t, sess, "host"), Value: enc(t, sess, "1.2.3.4"), Type: 0},
			{Name: enc(t, sess, "type"), Value: enc(t, sess, "ssh"), Type: 0},
		},
	}
	items, err := vaultadapter.NewSource("vw:personal", sess, []vaultclient.Cipher{ci}, cf).Items()
	if err != nil {
		t.Fatalf("Items: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("lowercase 'ssh' type should match, got %d items", len(items))
	}
}

// TestSourceDecryptLogin: lazy decrypt returns the original username+password.
func TestSourceDecryptLogin(t *testing.T) {
	sess := fakeSession(t)
	cf := config.Default().CustomFields
	ci := vaultclient.Cipher{
		ID:    "1",
		Name:  enc(t, sess, "prod-db"),
		Type:  1,
		Login: &vaultclient.Login{Username: enc(t, sess, "admin"), Password: enc(t, sess, "s3cret")},
		Fields: []vaultclient.CustomField{
			{Name: enc(t, sess, "host"), Value: enc(t, sess, "10.0.0.9"), Type: 0},
			{Name: enc(t, sess, "type"), Value: enc(t, sess, "SSH"), Type: 0},
		},
	}
	src := vaultadapter.NewSource("vw:personal", sess, []vaultclient.Cipher{ci}, cf)
	items, _ := src.Items()
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	user, pass, err := src.DecryptLogin(items[0])
	if err != nil {
		t.Fatalf("DecryptLogin: %v", err)
	}
	if string(user) != "admin" || string(pass) != "s3cret" {
		t.Errorf("user=%q pass=%q, want admin/s3cret", user, pass)
	}
}

func TestSourceUpdateCipherAndCipherByID(t *testing.T) {
	sess := fakeSession(t)
	cf := config.Default().CustomFields
	ci1 := vaultclient.Cipher{ID: "c1", Name: enc(t, sess, "server1")}
	ci2 := vaultclient.Cipher{ID: "c2", Name: enc(t, sess, "server2")}

	src := vaultadapter.NewSource("vw:personal", sess, []vaultclient.Cipher{ci1, ci2}, cf)

	got, ok := src.CipherByID("c2")
	if !ok || got.Name != ci2.Name {
		t.Errorf("CipherByID(c2) = (%+v, %v), want (%+v, true)", got, ok, ci2)
	}

	if _, ok := src.CipherByID("nonexistent"); ok {
		t.Errorf("expected false for nonexistent ID")
	}

	updated2 := vaultclient.Cipher{ID: "c2", Name: enc(t, sess, "server2-updated")}
	src.UpdateCipher(updated2)

	gotUpdated, ok := src.CipherByID("c2")
	if !ok || gotUpdated.Name != updated2.Name {
		t.Errorf("CipherByID after update = (%+v, %v), want (%+v, true)", gotUpdated, ok, updated2)
	}
}

