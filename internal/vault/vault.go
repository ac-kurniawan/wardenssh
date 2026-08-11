// Package vault is the seam between WardenSSH and the BitWarden/VaultWarden
// vault (Subsystem 2, .local/plan.md). The real native client — BitWarden
// Password Manager API auth (PBKDF2/Argon2, HKDF), 2FA TOTP/email, sync, and
// item decrypt (AES-256-CBC, HMAC, RSA key unwrap) — is deferred until a live
// VaultWarden instance is available to verify byte-identical decrypt against
// the `bw` CLI (per AGENTS.md: no untested crypto ships).
//
// This package defines the interface the rest of WardenSSH codes against and
// an in-memory fake for tests / future offline dev mode. The real client will
// satisfy the same interface with no call-site changes.
package vault

// Item is a decrypted BitWarden SSH-Key item as seen by the launcher.
type Item struct {
	ID   string
	Name string // display label (Q30/A: decrypted BitWarden item `name`)
	// Host connection directives, sourced from the item's custom fields.
	HostName  string
	User      string
	Port      string
	ProxyJump string
	// EncPrivateKey is the still-encrypted private key field from the vault.
	// Lazy-decrypted (Q8/C) at connect time via DecryptPrivateKey.
	EncPrivateKey string
	// EncPassphrase is the still-encrypted passphrase field (Q14/C).
	EncPassphrase string
}

// Source is one named, authenticated vault (a source label like "vw:personal").
type Source interface {
	Name() string
	// Items returns the launchable SSH-Key items — convention: only those with
	// a populated 'host' custom field (Q32/B). Item fields (Name, host, user,
	// port, proxyjump) are decrypted eagerly; the private key stays encrypted
	// until DecryptPrivateKey is called (lazy decrypt, Q8/C).
	Items() ([]Item, error)
	// DecryptPrivateKey decrypts the item's encrypted private key field into
	// raw private key bytes (PEM/OpenSSH) for loading into the agent. This is
	// called at connect time, not at list-build time.
	DecryptPrivateKey(item Item, passphrase string) ([]byte, error)
}

// Client is the multi-vault aggregate (Q16/B).
type Client interface {
	Sources() []Source
}

// --- in-memory fake for tests / dev ---

// FakeSource is a Source backed by a fixed in-memory item list.
type FakeSource struct {
	name  string
	items []Item
}

// NewFakeSource builds a fake source. Items without a populated HostName are
// filtered out by Items() per Q32/B.
func NewFakeSource(name string, items []Item) *FakeSource {
	return &FakeSource{name: name, items: items}
}

// Name satisfies Source.
func (s *FakeSource) Name() string { return s.name }

// Items satisfies Source: returns the items whose HostName is non-empty
// (Q32/B convention as opt-in), so the fake mirrors the real client's filter.
func (s *FakeSource) Items() ([]Item, error) {
	out := make([]Item, 0, len(s.items))
	for _, it := range s.items {
		if it.HostName == "" {
			continue
		}
		out = append(out, it)
	}
	return out, nil
}

// DecryptPrivateKey satisfies Source. For the fake, returns the EncPrivateKey
// as-is (it's typically pre-set with plaintext key bytes for tests).
func (s *FakeSource) DecryptPrivateKey(item Item, passphrase string) ([]byte, error) {
	return []byte(item.EncPrivateKey), nil
}

// FakeClient is a Client backed by a fixed list of Sources.
type FakeClient struct{ sources []Source }

// NewFakeClient builds a fake client from the given sources.
func NewFakeClient(sources ...Source) *FakeClient { return &FakeClient{sources: sources} }

// Sources satisfies Client.
func (c *FakeClient) Sources() []Source { return c.sources }