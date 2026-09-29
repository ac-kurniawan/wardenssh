// Package config loads and saves ~/.ssh/wardenssh.json (the non-secret config,
// per .local/spec.md Q16/B). Refresh tokens never live here — they live in the
// OS keyring (Q7/D). The file holds only: vault connection params, custom-field
// name overrides, UI prefs, and the keyring on/off toggle (mirrors --no-keyring).
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Vault is a single configured VaultWarden/BitWarden account (Q16/B multi-vault).
// Its refresh token is NOT stored here; only connection params.
type Vault struct {
	Name   string `json:"name"`
	Server string `json:"server"`
	Email  string `json:"email"`
}

// CustomFields holds the configurable names of the custom fields WardenSSH
// reads off BitWarden SSH-Key items (default "host"/"user"/"port"/"proxyjump").
// The "host" field's presence is the opt-in convention (Q32/B).
type CustomFields struct {
	Host      string `json:"host"`
	User      string `json:"user"`
	Port      string `json:"port"`
	ProxyJump string `json:"proxyjump"`
	// Type is the custom-field name whose value tags a Login item as an SSH
	// credential (value "SSH", case-insensitive). Logins without it are not
	// launchable.
	Type string `json:"type"`
}

// UI holds non-sensitive user preferences for the launcher.
type UI struct {
	Theme        string `json:"theme"`
	Sort         string `json:"sort"`
	LastSelected string `json:"last_selected"`
}

// Config is the full ~/.ssh/wardenssh.json document.
type Config struct {
	Vaults       []Vault      `json:"vaults,omitempty"`
	CustomFields CustomFields `json:"custom_fields"`
	UI           UI           `json:"ui,omitempty"`
	Keyring      bool         `json:"keyring"`
}

// Default returns a Config with the documented defaults applied.
func Default() *Config {
	return &Config{
		Vaults: nil,
		CustomFields: CustomFields{
			Host:      "host",
			User:      "user",
			Port:      "port",
			ProxyJump: "proxyjump",
			Type:      "type",
		},
		UI:      UI{Sort: "name"},
		Keyring: true,
	}
}

// Load reads a config from r and applies defaults for any missing fields.
func Load(r io.Reader) (*Config, error) {
	cfg := Default()
	dec := json.NewDecoder(r)
	if err := dec.Decode(cfg); err != nil {
		return nil, err
	}
	applyDefaults(cfg)
	return cfg, nil
}

// LoadFile reads a config from a path. A missing file is treated as a clean
// first-run config (with defaults) + no error — the TUI opens its setup modal
// on this signal per .local/spec.md.
func LoadFile(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Default(), nil
		}
		return nil, err
	}
	defer f.Close()
	return Load(f)
}

// Save writes a config to w as indented JSON (the file is hand-edited by users).
func Save(w io.Writer, cfg *Config) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(cfg)
}

// SaveFile writes a config to a path, creating the parent directory if needed.
func SaveFile(path string, cfg *Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	return Save(f, cfg)
}

// DefaultPath returns the conventional config path: ~/.ssh/wardenssh.json.
func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".ssh", "wardenssh.json"), nil
}

// ValidateVaultName enforces vault-name constraints: the name is the OS-keyring
// namespace, the host-list scope label, and a top-bar pill, so it must be
// non-empty after trimming, at most 32 runes, and free of control characters.
// The caller must store the trimmed form.
func ValidateVaultName(name string) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return errors.New("vault name is empty")
	}
	if utf8.RuneCountInString(trimmed) > 32 {
		return errors.New("vault name is longer than 32 characters")
	}
	for _, r := range trimmed {
		if unicode.IsControl(r) {
			return errors.New("vault name contains control characters")
		}
	}
	return nil
}

// SavePathFn persists a Config to a path (matches SaveFile's signature; the
// modal injects it so tests never touch the real ~/.ssh).
type SavePathFn func(path string, cfg *Config) error

// AddVault appends one vault to cfg and persists it via save. It validates the
// name (trimmed before storing) and rejects duplicates — vaults are unique by
// Name (the keyring namespace and scope label). The caller is expected to run
// it only after the vault's login+sync succeeded, so only verified entries
// reach the config file.
func AddVault(cfg *Config, v Vault, save SavePathFn, path string) error {
	v.Name = strings.TrimSpace(v.Name)
	if err := ValidateVaultName(v.Name); err != nil {
		return err
	}
	for _, existing := range cfg.Vaults {
		if existing.Name == v.Name {
			return fmt.Errorf("vault %q is already configured", v.Name)
		}
	}
	cfg.Vaults = append(cfg.Vaults, v)
	return save(path, cfg)
}

// applyDefaults fills any zero-valued custom-field name or UI sort with its
// default, so a partial config (e.g. only "host" overridden) still resolves
// the others. (Default()'s base values normally cover this, but explicit
// empty strings in the JSON would zero them out.)
func applyDefaults(cfg *Config) {
	d := Default().CustomFields
	if cfg.CustomFields.Host == "" {
		cfg.CustomFields.Host = d.Host
	}
	if cfg.CustomFields.User == "" {
		cfg.CustomFields.User = d.User
	}
	if cfg.CustomFields.Port == "" {
		cfg.CustomFields.Port = d.Port
	}
	if cfg.CustomFields.ProxyJump == "" {
		cfg.CustomFields.ProxyJump = d.ProxyJump
	}
	if cfg.CustomFields.Type == "" {
		cfg.CustomFields.Type = d.Type
	}
	if cfg.UI.Sort == "" {
		cfg.UI.Sort = "name"
	}
}
