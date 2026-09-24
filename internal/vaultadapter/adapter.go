// Package vaultadapter bridges the vaultclient (BitWarden API + crypto) into
// the vault.Source/Client interfaces that app.BuildHostList and the TUI
// consume. Each vault source is one authenticated vaultclient.Session; Items()
// decrypts item names + custom fields eagerly (for the host list) but leaves
// the private key encrypted for lazy decrypt at connect time (Q8/C).
package vaultadapter

import (
	"fmt"
	"strings"
	"sync"

	"github.com/ac-kurniawan/wardenssh/internal/config"
	"github.com/ac-kurniawan/wardenssh/internal/vault"
	"github.com/ac-kurniawan/wardenssh/internal/vaultclient"
	"github.com/ac-kurniawan/wardenssh/internal/vaultcrypto"
)

type Source struct {
	name    string
	session *vaultclient.Session
	fields  config.CustomFields // configurable custom-field names

	// mu guards ciphers and items. TriggerSync runs Sync from its own
	// goroutine while connect calls Items() from the UI goroutine — the
	// cache must not race.
	mu      sync.Mutex
	ciphers []vaultclient.Cipher

	// items caches the decrypted host list (names + custom fields). Connect
	// looks items up through Items() on every connect; without this cache that
	// re-decrypts the whole vault. Nil means stale. Cipher mutations clear it.
	items []vault.Item
}

// NewSource builds a Source from an authenticated session + sync ciphers.
// The custom-fields config maps BitWarden custom-field names to connection
// directives (host/user/port/proxyjump).
func NewSource(name string, sess *vaultclient.Session, ciphers []vaultclient.Cipher, fields config.CustomFields) *Source {
	return &Source{name: name, session: sess, ciphers: ciphers, fields: fields}
}

// Name satisfies vault.Source.
func (s *Source) Name() string { return s.name }

// cipherKeys resolves the key pair used to encrypt an item's fields. Legacy
// items use the account key directly; newer items carry a per-item key wrapped
// under the account key.
func (s *Source) cipherKeys(wrapped string) (enc, mac []byte, err error) {
	if wrapped == "" {
		return s.session.SymEnc, s.session.SymMac, nil
	}
	return vaultcrypto.UnwrapCipherKey(s.session.SymEnc, s.session.SymMac, wrapped)
}

// EncryptField encrypts one cipher field with the cipher's per-item key, or
// with the account key when wrappedKey is empty (legacy item).
func (s *Source) EncryptField(wrappedKey, plain string) (string, error) {
	encKey, macKey, err := s.cipherKeys(wrappedKey)
	if err != nil {
		return "", err
	}
	return vaultcrypto.Encrypt(encKey, macKey, []byte(plain))
}

// DecryptField decrypts one cipher field with the cipher's per-item key, or
// with the account key when wrappedKey is empty (legacy item).
func (s *Source) DecryptField(wrappedKey, encrypted string) ([]byte, error) {
	encKey, macKey, err := s.cipherKeys(wrappedKey)
	if err != nil {
		return nil, err
	}
	return vaultcrypto.Decrypt(encKey, macKey, encrypted)
}

// Items satisfies vault.Source: returns SSH-Key items with a populated 'host'
// custom field (Q32/B) plus Login items tagged type==SSH. Item names + custom
// fields are decrypted on the first call and cached; later calls (connect-time
// lookup) reuse that cache. The private key / login credentials stay encrypted
// (lazy decrypt, Q8/C).
func (s *Source) Items() ([]vault.Item, error) {
	s.mu.Lock()
	cached := s.items
	ciphers := s.ciphers
	s.mu.Unlock()
	if cached != nil {
		return copyItems(cached), nil
	}

	out := s.decryptItems(ciphers)

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.items != nil {
		// A concurrent Items() filled the cache first. Drop our copy so both
		// callers observe the same snapshot.
		return copyItems(s.items), nil
	}
	if s.ciphersChanged(ciphers) {
		// Sync or a cipher mutation landed while we were decrypting. Rebuild
		// against the ciphers we now hold so the cache matches them.
		out = s.decryptItems(s.ciphers)
	}
	s.items = out
	return copyItems(s.items), nil
}

// decryptItems builds the launchable host list from a cipher snapshot. It does
// not touch Source state, so it runs without mu held — decryption is the slow
// part and must not block Sync.
func (s *Source) decryptItems(ciphers []vaultclient.Cipher) []vault.Item {
	out := make([]vault.Item, 0, len(ciphers))
	for _, ci := range ciphers {
		// Resolve the key pair used to encrypt this item's fields: the account
		// key for legacy items, the item's own wrapped key (ci.Key) for newer
		// cipher-key-encrypted items.
		encKey, macKey, err := s.cipherKeys(ci.Key)
		if err != nil {
			continue // skip items whose per-item key won't unwrap
		}

		// Decrypt the item name (display label, Q30/A).
		nameBytes, err := decryptField(encKey, macKey, ci.Name)
		if err != nil {
			continue // skip items we can't decrypt
		}

		// Read custom fields via configurable names (Q16/B).
		cf := readCustomFields(encKey, macKey, ci.Fields, s.fields)

		// Q32/B: only items with a populated 'host' custom field are launchable.
		if cf.HostName == "" {
			continue
		}

		switch {
		case ci.Login != nil && ci.Login.Password != "" && strings.EqualFold(cf.Type, "ssh"):
			// Login item tagged type=SSH -> password-credential host.
			// Username is decrypted for display (User); the credentials stay
			// encrypted for lazy decrypt at connect time (Q8/C pattern).
			uname, _ := decryptField(encKey, macKey, ci.Login.Username)
			item := vault.Item{
				ID:          ci.ID,
				Name:        string(nameBytes),
				Kind:        "login",
				HostName:    cf.HostName,
				User:        string(uname),
				Port:        cf.Port,
				ProxyJump:   cf.ProxyJump,
				EncUsername: ci.Login.Username,
				EncPassword: ci.Login.Password,
				CipherKey:   ci.Key,
			}
			if item.User == "" {
				item.User = cf.User
			}
			out = append(out, item)
		case ci.SshKey != nil && ci.SshKey.PrivateKey != "":
			// SSH-Key item (existing path).
			item := vault.Item{
				ID:            ci.ID,
				Name:          string(nameBytes),
				HostName:      cf.HostName,
				User:          cf.User,
				Port:          cf.Port,
				ProxyJump:     cf.ProxyJump,
				EncPrivateKey: ci.SshKey.PrivateKey,
				CipherKey:     ci.Key,
			}
			if ci.SshKey.Passphrase != "" {
				item.EncPassphrase = ci.SshKey.Passphrase
			}
			out = append(out, item)
		}
	}
	return out
}

// ciphersChanged reports whether the cipher slice was replaced since the
// snapshot was taken. Caller must hold s.mu.
func (s *Source) ciphersChanged(snapshot []vaultclient.Cipher) bool {
	return len(s.ciphers) != len(snapshot) || (len(snapshot) > 0 && &s.ciphers[0] != &snapshot[0])
}

// copyItems returns a shallow copy so a later Sync cannot mutate the slice a
// caller is still reading.
func copyItems(in []vault.Item) []vault.Item {
	out := make([]vault.Item, len(in))
	copy(out, in)
	return out
}

// DecryptLogin satisfies vault.Source: lazily decrypts the item's native
// login username + password (Q8/C pattern). Called at connect time.
func (s *Source) DecryptLogin(item vault.Item) ([]byte, []byte, error) {
	encKey, macKey, err := s.cipherKeys(item.CipherKey)
	if err != nil {
		return nil, nil, fmt.Errorf("vaultadapter: unwrap cipher key: %w", err)
	}
	username, err := vaultcrypto.Decrypt(encKey, macKey, item.EncUsername)
	if err != nil {
		return nil, nil, fmt.Errorf("vaultadapter: decrypt login username: %w", err)
	}
	password, err := vaultcrypto.Decrypt(encKey, macKey, item.EncPassword)
	if err != nil {
		return nil, nil, fmt.Errorf("vaultadapter: decrypt login password: %w", err)
	}
	return username, password, nil
}

// DecryptPrivateKey satisfies vault.Source: lazily decrypts the item's private
// key field (Q8/C) using the session's symmetric key. Called at connect time.
func (s *Source) DecryptPrivateKey(item vault.Item, passphrase string) ([]byte, error) {
	encKey, macKey, err := s.cipherKeys(item.CipherKey)
	if err != nil {
		return nil, fmt.Errorf("vaultadapter: unwrap cipher key: %w", err)
	}
	decrypted, err := vaultcrypto.Decrypt(encKey, macKey, item.EncPrivateKey)
	if err != nil {
		return nil, fmt.Errorf("vaultadapter: decrypt private key: %w", err)
	}
	return decrypted, nil
}

// Sync re-fetches ciphers via the session API using the provided vaultclient.Client.
func (s *Source) Sync(c *vaultclient.Client) error {
	if s.session == nil {
		return fmt.Errorf("vaultadapter: nil session for source %s", s.name)
	}
	if c == nil {
		return fmt.Errorf("vaultadapter: nil client for source %s", s.name)
	}
	sr, err := c.Sync(s.session)
	if err != nil {
		return fmt.Errorf("vaultadapter: sync %s: %w", s.name, err)
	}
	s.mu.Lock()
	s.ciphers = sr.Ciphers
	s.invalidateItems()
	s.mu.Unlock()
	return nil
}

// customFieldValues holds decrypted custom-field values for one item.
type customFieldValues struct {
	HostName  string
	User      string
	Port      string
	ProxyJump string
	Type      string
}

// decryptCounter, when non-nil, is invoked once per field decrypt. Tests use
// it to prove Items() does not re-decrypt a cached vault. Production leaves it nil.
// decryptCounterMu guards the hook itself: go test -race runs packages' tests
// in parallel, so an unsynchronized package var races between tests.
var (
	decryptCounterMu sync.Mutex
	decryptCounter   func()
)

// SetDecryptCounter installs a test hook called on every field decrypt used
// to build the host list. The returned function restores the previous hook.
func SetDecryptCounter(fn func()) func() {
	decryptCounterMu.Lock()
	prev := decryptCounter
	decryptCounter = fn
	decryptCounterMu.Unlock()
	return func() {
		decryptCounterMu.Lock()
		decryptCounter = prev
		decryptCounterMu.Unlock()
	}
}

func decryptField(encKey, macKey []byte, enc string) ([]byte, error) {
	decryptCounterMu.Lock()
	hook := decryptCounter
	decryptCounterMu.Unlock()
	if hook != nil {
		hook()
	}
	return vaultcrypto.Decrypt(encKey, macKey, enc)
}

// invalidateItems drops the decrypted host-list cache. Caller must hold s.mu.
func (s *Source) invalidateItems() { s.items = nil }

// readCustomFields decrypts the cipher's custom fields and maps them to
// connection directives by name (configurable via config.CustomFields).
func readCustomFields(encKey, macKey []byte, fields []vaultclient.CustomField, cf config.CustomFields) customFieldValues {
	var v customFieldValues
	// Build a map of decrypted field-name → decrypted value.
	decrypted := make(map[string]string, len(fields))
	for _, f := range fields {
		if f.Value == "" {
			continue
		}
		nameBytes, err := decryptField(encKey, macKey, f.Name)
		if err != nil {
			continue
		}
		valBytes, err := decryptField(encKey, macKey, f.Value)
		if err != nil {
			continue
		}
		decrypted[string(nameBytes)] = string(valBytes)
	}
	v.HostName = decrypted[cf.Host]
	v.User = decrypted[cf.User]
	v.Port = decrypted[cf.Port]
	v.ProxyJump = decrypted[cf.ProxyJump]
	v.Type = decrypted[cf.Type]
	return v
}

// Client adapts multiple vaultclient sessions into vault.Client (Q16/B multi-vault).
type Client struct {
	sources []vault.Source
}

// NewClient builds a vault.Client from multiple authenticated sources.
func NewClient(sources ...*Source) *Client {
	out := make([]vault.Source, len(sources))
	for i, s := range sources {
		out[i] = s
	}
	return &Client{sources: out}
}

// Sources satisfies vault.Client.
func (c *Client) Sources() []vault.Source { return c.sources }

// Sync satisfies vault.Client: re-syncs all underlying sources without a client (no-op).
func (c *Client) Sync() error {
	return nil
}

// SyncAll re-syncs all underlying sources using the provided vaultclient.Client.
func (c *Client) SyncAll(vc *vaultclient.Client) error {
	for _, src := range c.sources {
		if s, ok := src.(*Source); ok {
			if err := s.Sync(vc); err != nil {
				return err
			}
		}
	}
	return nil
}

// SourceByName returns the Source with the given name (matching either "vw:<name>" or "<name>").
func (c *Client) SourceByName(name string) *Source {
	for _, src := range c.sources {
		if s, ok := src.(*Source); ok {
			if s.Name() == name || s.Name() == "vw:"+name || strings.TrimPrefix(s.Name(), "vw:") == strings.TrimPrefix(name, "vw:") {
				return s
			}
		}
	}
	return nil
}

// Session returns the underlying vaultclient.Session.
func (s *Source) Session() *vaultclient.Session { return s.session }

// Fields returns the configured custom-field mappings.
func (s *Source) Fields() config.CustomFields { return s.fields }

// AddCipher appends a newly created cipher to the source's cached ciphers.
func (s *Source) AddCipher(c vaultclient.Cipher) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ciphers = append(s.ciphers, c)
	s.invalidateItems()
}

// UpdateCipher replaces the cached cipher with matching ID, or appends if not found.
func (s *Source) UpdateCipher(c vaultclient.Cipher) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, existing := range s.ciphers {
		if existing.ID == c.ID {
			s.ciphers[i] = c
			s.invalidateItems()
			return
		}
	}
	s.ciphers = append(s.ciphers, c)
	s.invalidateItems()
}

// CipherByID returns the cached raw cipher matching the given ID.
func (s *Source) CipherByID(id string) (vaultclient.Cipher, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.ciphers {
		if c.ID == id {
			return c, true
		}
	}
	return vaultclient.Cipher{}, false
}

// RemoveCipher drops the cipher with the given id from the source's cached
// list. Called after a permanent delete so the deleted item never resurfaces
// from the local cache (e.g. when a later sync fails and the cache is kept).
// Removing an unknown id is a no-op.
func (s *Source) RemoveCipher(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, c := range s.ciphers {
		if c.ID == id {
			s.ciphers = append(s.ciphers[:i], s.ciphers[i+1:]...)
			s.invalidateItems()
			return
		}
	}
}

// Compile-time check: Source satisfies vault.Source and Client satisfies vault.Client.
var _ vault.Source = (*Source)(nil)
var _ vault.Client = (*Client)(nil)
