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