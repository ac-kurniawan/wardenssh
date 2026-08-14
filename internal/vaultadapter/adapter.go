// Package vaultadapter bridges the vaultclient (BitWarden API + crypto) into
// the vault.Source/Client interfaces that app.BuildHostList and the TUI
// consume. Each vault source is one authenticated vaultclient.Session; Items()
// decrypts item names + custom fields eagerly (for the host list) but leaves
// the private key encrypted for lazy decrypt at connect time (Q8/C).
package vaultadapter

import (
	"fmt"
	"strings"

	"github.com/ac-kurniawan/wardenssh/internal/config"
	"github.com/ac-kurniawan/wardenssh/internal/vault"
	"github.com/ac-kurniawan/wardenssh/internal/vaultclient"
)

// Source adapts a vaultclient.Session + sync ciphers into vault.Source.
type Source struct {
	name     string
	session  *vaultclient.Session
	ciphers  []vaultclient.Cipher
	fields   config.CustomFields // configurable custom-field names
}

// NewSource builds a Source from an authenticated session + sync ciphers.
// The custom-fields config maps BitWarden custom-field names to connection
// directives (host/user/port/proxyjump).
func NewSource(name string, sess *vaultclient.Session, ciphers []vaultclient.Cipher, fields config.CustomFields) *Source {
	return &Source{name: name, session: sess, ciphers: ciphers, fields: fields}
}

// Name satisfies vault.Source.
func (s *Source) Name() string { return s.name }

// Items satisfies vault.Source: returns SSH-Key items with a populated 'host'
// custom field (Q32/B) plus Login items tagged type==SSH. Item names + custom
// fields are decrypted eagerly; the private key / login credentials stay
// encrypted (lazy decrypt, Q8/C).
func (s *Source) Items() ([]vault.Item, error) {
	var out []vault.Item
	for _, ci := range s.ciphers {
		// Decrypt the item name (display label, Q30/A).
		nameBytes, err := s.session.DecryptField(ci.Name)
		if err != nil {
			continue // skip items we can't decrypt
		}

		// Read custom fields via configurable names (Q16/B).
		cf := readCustomFields(s.session, ci.Fields, s.fields)

		// Q32/B: only items with a populated 'host' custom field are launchable.
		if cf.HostName == "" {
			continue
		}

		switch {
		case ci.Login != nil && ci.Login.Password != "" && strings.EqualFold(cf.Type, "ssh"):
			// Login item tagged type=SSH -> password-credential host.
			// Username is decrypted for display (User); the credentials stay
			// encrypted for lazy decrypt at connect time (Q8/C pattern).
			uname, _ := s.session.DecryptField(ci.Login.Username)
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
			}
			if ci.SshKey.Passphrase != "" {
				item.EncPassphrase = ci.SshKey.Passphrase
			}
			out = append(out, item)
		}
	}
	return out, nil
}

// DecryptLogin satisfies vault.Source: lazily decrypts the item's native
// login username + password (Q8/C pattern). Called at connect time.
func (s *Source) DecryptLogin(item vault.Item) ([]byte, []byte, error) {
	username, err := s.session.DecryptField(item.EncUsername)
	if err != nil {
		return nil, nil, fmt.Errorf("vaultadapter: decrypt login username: %w", err)
	}
	password, err := s.session.DecryptField(item.EncPassword)
	if err != nil {
		return nil, nil, fmt.Errorf("vaultadapter: decrypt login password: %w", err)
	}
	return username, password, nil
}

// DecryptPrivateKey satisfies vault.Source: lazily decrypts the item's private
// key field (Q8/C) using the session's symmetric key. Called at connect time.
func (s *Source) DecryptPrivateKey(item vault.Item, passphrase string) ([]byte, error) {
	decrypted, err := s.session.DecryptField(item.EncPrivateKey)
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
	s.ciphers = sr.Ciphers
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

// readCustomFields decrypts the cipher's custom fields and maps them to
// connection directives by name (configurable via config.CustomFields).
func readCustomFields(sess *vaultclient.Session, fields []vaultclient.CustomField, cf config.CustomFields) customFieldValues {
	var v customFieldValues
	// Build a map of decrypted field-name → decrypted value.
	decrypted := make(map[string]string, len(fields))
	for _, f := range fields {
		if f.Value == "" {
			continue
		}
		nameBytes, err := sess.DecryptField(f.Name)
		if err != nil {
			continue
		}
		valBytes, err := sess.DecryptField(f.Value)
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
	s.ciphers = append(s.ciphers, c)
}

// RemoveCipher drops the cipher with the given id from the source's cached
// list. Called after a permanent delete so the deleted item never resurfaces
// from the local cache (e.g. when a later sync fails and the cache is kept).
// Removing an unknown id is a no-op.
func (s *Source) RemoveCipher(id string) {
	for i, c := range s.ciphers {
		if c.ID == id {
			s.ciphers = append(s.ciphers[:i], s.ciphers[i+1:]...)
			return
		}
	}
}

// Compile-time check: Source satisfies vault.Source and Client satisfies vault.Client.
var _ vault.Source = (*Source)(nil)
var _ vault.Client = (*Client)(nil)

