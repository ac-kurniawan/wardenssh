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