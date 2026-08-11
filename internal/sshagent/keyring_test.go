package sshagent

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"testing"
)

// generateEd25519 returns a fresh ed25519 private key for tests.
func generateEd25519(t *testing.T) *ed25519.PrivateKey {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519 generate: %v", err)
	}
	return &priv
}

func generateRSA(t *testing.T, bits int) *rsa.PrivateKey {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		t.Fatalf("rsa generate: %v", err)
	}
	return k
}

func generateECDSA(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa generate: %v", err)
	}
	return k
}

// TestKeyringLoadAndList: loading a key for a session makes it listable via
// the agent protocol, with the comment preserved.
func TestKeyringLoadAndList(t *testing.T) {
	kr := NewKeyring()
	priv := generateEd25519(t)

	if _, err := kr.Load(priv, "prod-db-01", "sess-A"); err != nil {
		t.Fatalf("Load: %v", err)
	}

	keys, err := kr.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("want 1 key, got %d", len(keys))
	}
	if keys[0].Comment != "prod-db-01" {
		t.Errorf("comment = %q, want prod-db-01", keys[0].Comment)
	}
}

// TestKeyringReleaseLastSessionUnloadsKey: per Q19/B, releasing the last
// session holding a key removes it from the keyring.
func TestKeyringReleaseLastSessionUnloadsKey(t *testing.T) {
	kr := NewKeyring()
	priv := generateEd25519(t)

	if _, err := kr.Load(priv, "k1", "sess-A"); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := kr.ReleaseSession("sess-A"); err != nil {
		t.Fatalf("ReleaseSession: %v", err)
	}

	keys, err := kr.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(keys) != 0 {
		t.Fatalf("want 0 keys after last session released, got %d", len(keys))
	}
}

// TestKeyringRefCountedAcrossSessions: two sessions using the same key —
// releasing one keeps the key; releasing both removes it.
func TestKeyringRefCountedAcrossSessions(t *testing.T) {
	kr := NewKeyring()
	port := generateRSA(t, 1024) // small key for test speed

	pub1, err := kr.Load(port, "shared", "sess-A")
	if err != nil {
		t.Fatalf("Load A: %v", err)
	}
	if _, err := kr.Load(port, "shared", "sess-B"); err != nil {
		t.Fatalf("Load B: %v", err)
	}

	// Same public key both times.
	if _, err := kr.Load(port, "shared", "sess-B"); err != nil {
		t.Fatalf("Load B again: %v", err)
	}
	keys, _ := kr.List()
	if len(keys) != 1 {
		t.Fatalf("want 1 shared key (dedup), got %d", len(keys))
	}

	// Release A — key must remain because B still holds it.
	if err := kr.ReleaseSession("sess-A"); err != nil {
		t.Fatalf("Release A: %v", err)
	}
	keys, _ = kr.List()
	if len(keys) != 1 {
		t.Fatalf("want key still held by sess-B, got %d", len(keys))
	}
	_ = pub1

	// Release B — now the key should be gone.
	if err := kr.ReleaseSession("sess-B"); err != nil {
		t.Fatalf("Release B: %v", err)
	}
	keys, _ = kr.List()
	if len(keys) != 0 {
		t.Fatalf("want 0 keys after all sessions released, got %d", len(keys))
	}
}

// TestKeyringAllKeyTypes: Q20/C requires ed25519, RSA, and ECDSA all load,
// list, and sign in v0. Verifies each is recognized by ssh.NewSignerFromKey.
func TestKeyringAllKeyTypes(t *testing.T) {
	cases := []struct {
		name   string
		priv   interface{}
		Format string // expected ssh Format prefix in List output
	}{
		{"ed25519", generateEd25519(t), "ssh-ed25519"},
		{"rsa", generateRSA(t, 1024), "ssh-rsa"},
		{"ecdsa", generateECDSA(t), "ecdsa-sha2-nistp256"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			kr := NewKeyring()
			pub, err := kr.Load(c.priv, c.name, "s")
			if err != nil {
				t.Fatalf("Load %s: %v", c.name, err)
			}
			keys, _ := kr.List()
			if len(keys) != 1 {
				t.Fatalf("%s: want 1 key, got %d", c.name, len(keys))
			}
			if got := keys[0].Format; got != c.Format {
				t.Errorf("%s: Format = %q, want %q", c.name, got, c.Format)
			}
			sig, err := kr.Sign(pub, []byte("data"))
			if err != nil {
				t.Fatalf("%s: Sign: %v", c.name, err)
			}
			if err := pub.Verify([]byte("data"), sig); err != nil {
				t.Errorf("%s: verify: %v", c.name, err)
			}
		})
	}
}

// TestKeyringReleaseUnknownSessionIsNoop: releasing a session that never
// held a key must not panic and must not corrupt state.
func TestKeyringReleaseUnknownSessionIsNoop(t *testing.T) {
	kr := NewKeyring()
	if err := kr.ReleaseSession("nope"); err != nil {
		t.Errorf("ReleaseSession unknown: want nil, got %v", err)
	}
}

// TestKeyringSign: a loaded key must produce a valid ssh signature over
// given data, verifiable against the corresponding public key.
func TestKeyringSign(t *testing.T) {
	kr := NewKeyring()
	priv := generateEd25519(t)

	pub, err := kr.Load(priv, "k", "sess-A")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	data := []byte("hello wardenssh")
	sig, err := kr.Sign(pub, data)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if err := pub.Verify(data, sig); err != nil {
		t.Errorf("signature verify: %v", err)
	}
}

// TestKeyringLoadRejectsNil: defensive — loading a nil key returns an error
// rather than panicking inside the signer.
func TestKeyringLoadRejectsNil(t *testing.T) {
	kr := NewKeyring()
	if _, err := kr.Load(nil, "k", "s"); err == nil {
		t.Errorf("Load(nil): want error, got nil")
	}
}

// TestKeyringDuplicateLoadSameSession: loading the same key twice from the
// same session must not double-count the ref (resource leak on release).
func TestKeyringDuplicateLoadSameSession(t *testing.T) {
	kr := NewKeyring()
	port := generateRSA(t, 1024)

	if _, err := kr.Load(port, "k", "sess-A"); err != nil {
		t.Fatalf("Load A1: %v", err)
	}
	if _, err := kr.Load(port, "k", "sess-A"); err != nil {
		t.Fatalf("Load A2: %v", err)
	}
	// A single release should drop the key (idempotent load within a session).
	if err := kr.ReleaseSession("sess-A"); err != nil {
		t.Fatalf("Release: %v", err)
	}
	keys, _ := kr.List()
	if len(keys) != 0 {
		t.Errorf("want 0 keys, got %d (ref leak)", len(keys))
	}
}

