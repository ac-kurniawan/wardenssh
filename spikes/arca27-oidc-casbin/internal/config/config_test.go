package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTempConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

const minimal = `{
  "server": {"port": 7777, "base_url": "http://localhost:7777"},
  "oidc": {"scopes": ["openid", "profile", "email", "offline_access"], "cookie_name": "arca27_session"},
  "authz": {"model_path": "deploy/model.conf", "policy_path": "deploy/policy.csv"}
}`

func TestLoadDefaults(t *testing.T) {
	t.Setenv("OIDC_ISSUER", "http://idp.test")
	t.Setenv("OIDC_CLIENT_ID", "cid")
	t.Setenv("OIDC_REDIRECT_URI", "http://app.test/auth/callback")

	cfg, err := Load(writeTempConfig(t, minimal))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Server.Port != 7777 {
		t.Errorf("port = %d, want 7777", cfg.Server.Port)
	}
	if cfg.OIDC.CookieName != "arca27_session" {
		t.Errorf("cookie = %q", cfg.OIDC.CookieName)
	}
	if cfg.Authz.ModelPath != "deploy/model.conf" {
		t.Errorf("model path = %q", cfg.Authz.ModelPath)
	}
}

func TestLoadEnvOverrides(t *testing.T) {
	t.Setenv("OIDC_ISSUER", "http://idp.example:1234")
	t.Setenv("OIDC_CLIENT_ID", "client-42")
	t.Setenv("OIDC_REDIRECT_URI", "http://app.example:4242/auth/callback")
	t.Setenv("APP_PORT", "4242")
	t.Setenv("APP_BASE_URL", "http://app.example:4242")
	t.Setenv("APP_SECRET", "irrelevant")

	cfg, err := Load(writeTempConfig(t, minimal))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.OIDC.Issuer != "http://idp.example:1234" {
		t.Errorf("issuer = %q, want env override", cfg.OIDC.Issuer)
	}
	if cfg.OIDC.ClientID != "client-42" {
		t.Errorf("client id = %q, want env override", cfg.OIDC.ClientID)
	}
	if cfg.OIDC.RedirectURI != "http://app.example:4242/auth/callback" {
		t.Errorf("redirect uri = %q, want env override", cfg.OIDC.RedirectURI)
	}
}

func TestLoadRejectsIncomplete(t *testing.T) {
	// No OIDC_* env and no issuer in config.json -> must fail loudly.
	if _, err := Load(writeTempConfig(t, minimal)); err == nil {
		t.Fatal("expected error for missing issuer/client/redirect, got nil")
	}
}
