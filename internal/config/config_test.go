package config_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ac-kurniawan/wardenssh/internal/config"
)

// TestLoadAppliesDefaults: a minimal config with only vaults parses and fills
// in default custom-field names and the default keyring=true (Q16/B, Q7/D).
func TestLoadAppliesDefaults(t *testing.T) {
	in := `{"vaults":[{"name":"personal","server":"https://vw.example.com","email":"me@x"}]}`
	cfg, err := config.Load(strings.NewReader(in))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.CustomFields.Host != "host" {
		t.Errorf("CustomFields.Host default = %q, want host", cfg.CustomFields.Host)
	}
	if cfg.CustomFields.User != "user" {
		t.Errorf("CustomFields.User default = %q, want user", cfg.CustomFields.User)
	}
	if cfg.CustomFields.Port != "port" {
		t.Errorf("CustomFields.Port default = %q, want port", cfg.CustomFields.Port)
	}
	if cfg.CustomFields.ProxyJump != "proxyjump" {
		t.Errorf("CustomFields.ProxyJump default = %q, want proxyjump", cfg.CustomFields.ProxyJump)
	}
	if !cfg.Keyring {
		t.Error("Keyring default = false, want true")
	}
}

// TestLoadParsesVaultsAndOverrides: explicit vaults and custom_fields overrides
// are honored verbatim.
func TestLoadParsesVaultsAndOverrides(t *testing.T) {
	in := `{
		"vaults":[
			{"name":"personal","server":"https://vw.example.com","email":"me@x"},
			{"name":"work","server":"https://vault.corp","email":"me@work"}
		],
		"custom_fields":{"host":"address","user":"login","port":"sshport","proxyjump":"jump"},
		"ui":{"theme":"dark","sort":"name","last_selected":"web-02"},
		"keyring":false
	}`
	cfg, err := config.Load(strings.NewReader(in))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Vaults) != 2 {
		t.Fatalf("want 2 vaults, got %d", len(cfg.Vaults))
	}
	if cfg.Vaults[1].Name != "work" || cfg.Vaults[1].Server != "https://vault.corp" || cfg.Vaults[1].Email != "me@work" {
		t.Errorf("vault[1] = %+v", cfg.Vaults[1])
	}
	if cfg.CustomFields.Host != "address" {
		t.Errorf("CustomFields.Host = %q, want address", cfg.CustomFields.Host)
	}
	if cfg.UI.Theme != "dark" || cfg.UI.Sort != "name" || cfg.UI.LastSelected != "web-02" {
		t.Errorf("UI = %+v", cfg.UI)
	}
	if cfg.Keyring {
		t.Error("Keyring = true, want false (explicit override)")
	}
}

// TestSaveRoundTrip: Save then Load reproduces the config, and the serialized
// form contains NO token/password/secret fields (security invariant: no
// secrets in ~/.ssh/wardenssh.json per AGENTS.md).
func TestSaveRoundTrip(t *testing.T) {
	cfg := config.Default()
	cfg.Vaults = []config.Vault{{Name: "p", Server: "https://vw", Email: "a@b"}}
	cfg.UI.Sort = "name"

	var buf bytes.Buffer
	if err := config.Save(&buf, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	out := buf.String()
	for _, bad := range []string{"token", "password", "secret", "passphrase", "refresh"} {
		if strings.Contains(strings.ToLower(out), bad) {
			t.Errorf("serialized config contains %q: %s", bad, out)
		}
	}

	round, err := config.Load(&buf)
	if err != nil {
		t.Fatalf("re-Load: %v", err)
	}
	if round.Vaults[0].Name != "p" || round.CustomFields.Host != "host" || round.UI.Sort != "name" || !round.Keyring {
		t.Errorf("round-trip mismatch: %+v", round)
	}
}

// TestDefaultIsClean: Default() produces a config with no vaults and only
// non-secret defaults, and it is valid JSON that Load accepts.
func TestDefaultIsClean(t *testing.T) {
	cfg := config.Default()
	if len(cfg.Vaults) != 0 {
		t.Errorf("Default has %d vaults, want 0", len(cfg.Vaults))
	}
	var buf bytes.Buffer
	if err := config.Save(&buf, cfg); err != nil {
		t.Fatalf("Save default: %v", err)
	}
	if _, err := config.Load(&buf); err != nil {
		t.Errorf("re-Load default: %v", err)
	}
}

// TestSaveIsIndentedJSON: output is human-readable indented JSON (file is the
// only config surface; users edit it by hand per spec).
func TestSaveIsIndentedJSON(t *testing.T) {
	cfg := config.Default()
	var buf bytes.Buffer
	if err := config.Save(&buf, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	out := buf.String()
	dec := json.NewDecoder(strings.NewReader(out))
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		t.Errorf("not valid JSON: %v", err)
	}
	// Indented JSON contains a newline+indent; a bare "{}" would be < 20 chars.
	if len(out) < 20 || !strings.Contains(out, "\n  ") {
		t.Errorf("expected indented default JSON, got %q", out)
	}
}

// TestLoadMalformedReturnsError: malformed JSON is an error, not a silent empty config.
func TestLoadMalformedReturnsError(t *testing.T) {
	if _, err := config.Load(strings.NewReader(`{not json`)); err == nil {
		t.Error("Load malformed: want error, got nil")
	}
}

// TestLoadAppliesTypeFieldDefault: a minimal config gets default type="type".
func TestLoadAppliesTypeFieldDefault(t *testing.T) {
	cfg, err := config.Load(strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.CustomFields.Type != "type" {
		t.Errorf("CustomFields.Type default = %q, want type", cfg.CustomFields.Type)
	}
}

// TestLoadParsesTypeOverride: an explicit custom_fields.type is honored.
func TestLoadParsesTypeOverride(t *testing.T) {
	cfg, err := config.Load(strings.NewReader(`{"custom_fields":{"type":"kind"}}`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.CustomFields.Type != "kind" {
		t.Errorf("CustomFields.Type = %q, want kind", cfg.CustomFields.Type)
	}
}

// TestValidateVaultName: vault names are the keyring namespace, scope label,
// and top-bar pill, so they must be non-empty after trimming, ≤32 runes, and
// free of control characters. Valid names return nil.
func TestValidateVaultName(t *testing.T) {
	cases := []struct {
		name string
		want string // "" means valid
	}{
		{"vw", ""},
		{"Work Vault", ""},
		{"", "empty"},
		{"   ", "empty"},
		{"  padded  ", ""},   // trimmed internally; caller stores the trimmed form
		{"a-" + strings.Repeat("x", 30), ""}, // exactly 32 runes
		{"a-" + strings.Repeat("x", 31), "too long"}, // 33 runes
		{"bad\x00name", "control"},
		{"bad\ttab", "control"},
	}
	for _, c := range cases {
		err := config.ValidateVaultName(c.name)
		if c.want == "" && err != nil {
			t.Errorf("ValidateVaultName(%q) = %v, want nil", c.name, err)
		}
		if c.want != "" && err == nil {
			t.Errorf("ValidateVaultName(%q) = nil, want error (%s)", c.name, c.want)
		}
	}
}

// TestAddVaultAppendsAndSaves: AddVault validates the name, rejects duplicates
// (vaults are unique by Name), appends to the existing list preserving other
// vaults and settings, and saves via the injected path.
func TestAddVaultAppendsAndSaves(t *testing.T) {
	cfg := config.Default()
	cfg.Vaults = []config.Vault{{Name: "vw", Server: "https://vw.example.com", Email: "me@x"}}

	saved := ""
	savePath := func(p string, c *config.Config) error {
		saved = p
		var buf bytes.Buffer
		if err := config.Save(&buf, c); err != nil {
			return err
		}
		cfg = c
		return nil
	}

	err := config.AddVault(cfg, config.Vault{Name: "work", Server: "https://vw2.example.com", Email: "w@x"}, savePath, "/tmp/wardenssh.json")
	if err != nil {
		t.Fatalf("AddVault: %v", err)
	}
	if saved != "/tmp/wardenssh.json" {
		t.Errorf("save path = %q, want /tmp/wardenssh.json", saved)
	}
	if len(cfg.Vaults) != 2 || cfg.Vaults[1].Name != "work" {
		t.Fatalf("vaults after add = %+v, want [vw work]", cfg.Vaults)
	}
	if cfg.Vaults[0].Name != "vw" || cfg.CustomFields.Host != "host" || !cfg.Keyring {
		t.Errorf("existing vaults/settings clobbered: %+v", cfg)
	}
}

// TestAddVaultRejectsDuplicateName: vaults are unique by Name (the keyring
// namespace and scope label) — a second add with the same name is an error,
// not an overwrite.
func TestAddVaultRejectsDuplicateName(t *testing.T) {
	cfg := config.Default()
	cfg.Vaults = []config.Vault{{Name: "vw", Server: "https://a", Email: "a@x"}}
	err := config.AddVault(cfg, config.Vault{Name: "vw", Server: "https://b", Email: "b@x"}, func(string, *config.Config) error { return nil }, "/tmp/x.json")
	if err == nil {
		t.Fatal("AddVault duplicate name = nil, want error")
	}
	if len(cfg.Vaults) != 1 {
		t.Errorf("vaults mutated on rejected add: %+v", cfg.Vaults)
	}
}

// TestAddVaultRejectsInvalidName: an invalid vault name fails before any save.
func TestAddVaultRejectsInvalidName(t *testing.T) {
	cfg := config.Default()
	called := false
	err := config.AddVault(cfg, config.Vault{Name: "  ", Server: "https://a", Email: "a@x"}, func(string, *config.Config) error { called = true; return nil }, "/tmp/x.json")
	if err == nil {
		t.Fatal("AddVault blank name = nil, want error")
	}
	if called {
		t.Error("save called despite invalid name")
	}
}
