package sshagent

import (
	"testing"

	"golang.org/x/crypto/ssh/agent"
)

// Compile-time check: Keyring implements the full agent.Agent interface so
// agent.ServeAgent can drive it over a connection.
var _ agent.Agent = (*Keyring)(nil)

// TestKeyringProtocolManagementDisabled: the agent protocol's Add/Remove/
// RemoveAll paths must be refused — keys enter the keyring ONLY via the
// vault's programmatic Load (lazy-decrypt), never through the wire
// protocol. This prevents a stray `ssh-add` from injecting keys into
// WardenSSH's agent.
func TestKeyringProtocolManagementDisabled(t *testing.T) {
	kr := NewKeyring()
	priv := generateEd25519(t)

	if err := kr.Add(agent.AddedKey{PrivateKey: priv}); err == nil {
		t.Errorf("Add: want error (protocol management disabled), got nil")
	}
	pub, _ := kr.Load(priv, "k", "s")
	if err := kr.Remove(pub); err == nil {
		t.Errorf("Remove: want error, got nil")
	}
	if err := kr.RemoveAll(); err == nil {
		t.Errorf("RemoveAll: want error, got nil")
	}
	// Lock/Unlock likewise disabled in v0 (TBD whether keys ever get locked
	// in RAM; deferred, see .local/spec.md Q22/C session-only cache).
	if err := kr.Lock(nil); err == nil {
		t.Errorf("Lock: want error, got nil")
	}
	if err := kr.Unlock(nil); err == nil {
		t.Errorf("Unlock: want error, got nil")
	}
}

// TestKeyringSigners: Signers returns the loaded keys as ssh.Signers for the
// agent protocol's signing path.
func TestKeyringSigners(t *testing.T) {
	kr := NewKeyring()
	priv := generateEd25519(t)
	if _, err := kr.Load(priv, "k", "s"); err != nil {
		t.Fatalf("Load: %v", err)
	}
	signers, err := kr.Signers()
	if err != nil {
		t.Fatalf("Signers: %v", err)
	}
	if len(signers) != 1 {
		t.Fatalf("want 1 signer, got %d", len(signers))
	}
}