// Package sshagent implements WardenSSH's in-process ssh-agent (see
// .local/spec.md): a keyring holding private keys in RAM, served over a
// pipe to ssh.exe. It is reference-counted per session (Q19/B) — a key is
// unloaded from RAM when the last session using it disconnects.
package sshagent

import (
	"errors"
	"fmt"
	"sync"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// errDisabled is returned for all protocol-driven key management operations
// (Add/Remove/RemoveAll/Lock/Unlock). Keys enter and leave the keyring only
// via Load/ReleaseSession (vault lazy-decrypt + session ref-counting).
var errDisabled = errors.New("sshagent: protocol key management disabled; keys are loaded from the vault")

// Keyring holds loaded private keys in RAM and serves the agent protocol.
// It wraps golang.org/x/crypto/ssh/agent's in-memory keyring for the actual
// protocol operations (List/Sign/Signers) and adds per-session reference
// counting so keys are removed when no live session holds them.
type Keyring struct {
	mu   sync.Mutex
	inner agent.Agent
	refs  map[string]*keyEntry // fingerprint -> entry
}

type keyEntry struct {
	pub     ssh.PublicKey
	holders map[string]struct{} // set of session IDs holding this key
}

// NewKeyring creates an empty reference-counted keyring.
func NewKeyring() *Keyring {
	return &Keyring{
		inner: agent.NewKeyring(),
		refs:  make(map[string]*keyEntry),
	}
}

// Load makes a (decrypted) private key available to the agent for the given
// session. If the key is already loaded (by any session), it only records the
// new session as an additional holder (idempotent for the same session).
// Returns the key's ssh.PublicKey for use in agent Sign requests.
func (k *Keyring) Load(priv interface{}, comment, session string) (ssh.PublicKey, error) {
	if priv == nil {
		return nil, errors.New("sshagent: nil private key")
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		return nil, fmt.Errorf("sshagent: build signer: %w", err)
	}
	pub := signer.PublicKey()
	fp := ssh.FingerprintSHA256(pub)

	k.mu.Lock()
	defer k.mu.Unlock()

	entry, ok := k.refs[fp]
	if !ok {
		// First holder: add the key to the underlying agent keyring.
		if err := k.inner.Add(agent.AddedKey{
			PrivateKey: priv,
			Comment:    comment,
		}); err != nil {
			return nil, fmt.Errorf("sshagent: add key to agent: %w", err)
		}
		entry = &keyEntry{
			pub:     pub,
			holders: map[string]struct{}{session: {}},
		}
		k.refs[fp] = entry
	} else {
		entry.holders[session] = struct{}{}
	}
	return pub, nil
}

// ReleaseSession drops all key-holds for the given session. Any key whose
// holder set becomes empty is removed from the agent (unloaded from RAM).
// Releasing an unknown session is a no-op.
func (k *Keyring) ReleaseSession(session string) error {
	k.mu.Lock()
	defer k.mu.Unlock()

	for fp, entry := range k.refs {
		delete(entry.holders, session)
		if len(entry.holders) == 0 {
			if err := k.inner.Remove(entry.pub); err != nil {
				// Best-effort: log-agnostic removal. Continue dropping the
				// bookkeeping entry regardless so refs do not leak.
				_ = err
			}
			delete(k.refs, fp)
		}
	}
	return nil
}

// List returns the keys currently available to the agent (agent protocol).
func (k *Keyring) List() ([]*agent.Key, error) {
	return k.inner.List()
}

// Sign produces an ssh signature over data using the named key (agent protocol).
func (k *Keyring) Sign(key ssh.PublicKey, data []byte) (*ssh.Signature, error) {
	return k.inner.Sign(key, data)
}

// Signers returns the loaded keys as ssh.Signers (agent protocol).
func (k *Keyring) Signers() ([]ssh.Signer, error) {
	return k.inner.Signers()
}

// Add refuses keys arriving via the agent wire protocol. WardenSSH loads
// keys only from the vault via Load (lazy-decrypt); a stray `ssh-add` must
// not inject keys into the in-process agent.
func (k *Keyring) Add(_ agent.AddedKey) error { return errDisabled }

// Remove refuses protocol-driven key removal. Keys leave the keyring only
// when their last holding session disconnects (ReleaseSession).
func (k *Keyring) Remove(_ ssh.PublicKey) error { return errDisabled }

// RemoveAll refuses protocol-driven removal of all keys.
func (k *Keyring) RemoveAll() error { return errDisabled }

// Lock is disabled in v0 (the session-only passphrase cache lives in the
// TUI process, not the agent keyring; see .local/spec.md Q22/C).
func (k *Keyring) Lock(_ []byte) error { return errDisabled }

// Unlock is the counterpart to Lock; disabled in v0.
func (k *Keyring) Unlock(_ []byte) error { return errDisabled }