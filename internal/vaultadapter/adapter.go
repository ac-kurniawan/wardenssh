// Package vaultadapter bridges the vaultclient (BitWarden API + crypto) into
// the vault.Source/Client interfaces that app.BuildHostList and the TUI
// consume. Each vault source is one authenticated vaultclient.Session; Items()
// decrypts item names + custom fields eagerly (for the host list) but leaves
// the private key encrypted for lazy decrypt at connect time (Q8/C).
package vaultadapter

import (
	"fmt"

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
// custom field (Q32/B). Item names + custom fields are decrypted eagerly; the
// private key stays encrypted (lazy decrypt, Q8/C).
func (s *Source) Items() ([]vault.Item, error) {
	var out []vault.Item
	for _, ci := range s.ciphers {
		if ci.SshKey == nil || ci.SshKey.PrivateKey == "" {
			continue
		}

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
	return out, nil
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

// customFieldValues holds decrypted custom-field values for one item.
type customFieldValues struct {
	HostName  string
	User      string
	Port      string
	ProxyJump string
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

// Compile-time check: Source satisfies vault.Source.
var _ vault.Source = (*Source)(nil)
var _ vault.Client = (*Client)(nil)