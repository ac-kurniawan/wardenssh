package vaultadapter_test

import (
	"crypto/rand"
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
			SshKey: &struct {
				PrivateKey     string `json:"privateKey"`
				PublicKey      string `json:"publicKey"`
				KeyFingerprint string `json:"keyFingerprint"`
				Passphrase     string `json:"passphrase"`
			}{PrivateKey: enc(t, sess, "PRIVATE-KEY-BYTES-1")},
			Fields: []vaultclient.CustomField{
				{Name: enc(t, sess, "host"), Value: enc(t, sess, "10.0.0.5"), Type: 0},
				{Name: enc(t, sess, "user"), Value: enc(t, sess, "admin"), Type: 0},
			},
		},
		{
			ID:   "2",
			Name: enc(t, sess, "no-host-item"),
			SshKey: &struct {
				PrivateKey     string `json:"privateKey"`
				PublicKey      string `json:"publicKey"`
				KeyFingerprint string `json:"keyFingerprint"`
				Passphrase     string `json:"passphrase"`
			}{PrivateKey: enc(t, sess, "PRIVATE-KEY-BYTES-2")},
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
			SshKey: &struct {
				PrivateKey     string `json:"privateKey"`
				PublicKey      string `json:"publicKey"`
				KeyFingerprint string `json:"keyFingerprint"`
				Passphrase     string `json:"passphrase"`
			}{PrivateKey: encPriv},
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