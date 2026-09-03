package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/viper"
)

// Config holds the merged configuration for the spike server.
type Config struct {
	Server struct {
		Port    int    `mapstructure:"port"`
		BaseURL string `mapstructure:"base_url"`
	} `mapstructure:"server"`

	OIDC struct {
		Issuer       string   `mapstructure:"issuer"`
		ClientID     string   `mapstructure:"client_id"`
		ClientSecret string   `mapstructure:"client_secret"`
		RedirectURI  string   `mapstructure:"redirect_uri"`
		Scopes       []string `mapstructure:"scopes"`
		CookieName   string   `mapstructure:"cookie_name"`
	} `mapstructure:"oidc"`

	Authz struct {
		ModelPath  string `mapstructure:"model_path"`
		PolicyPath string `mapstructure:"policy_path"`
	} `mapstructure:"authz"`
}

// Load reads deploy/config.json and merges environment overrides.
// Environment variables (documented in README):
//
//	OIDC_ISSUER, OIDC_CLIENT_ID, OIDC_CLIENT_SECRET, OIDC_REDIRECT_URI
//	APP_PORT, APP_BASE_URL
func Load(configPath string) (*Config, error) {
	v := viper.New()
	v.SetConfigFile(configPath)
	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	v.AutomaticEnv()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	for _, key := range []string{
		"server.port", "server.base_url",
		"oidc.issuer", "oidc.client_id", "oidc.client_secret", "oidc.redirect_uri", "oidc.scopes",
	} {
		_ = v.BindEnv(key)
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	// Flat env fallbacks used by the provision script (.env.local).
	if cfg.OIDC.Issuer == "" {
		cfg.OIDC.Issuer = os.Getenv("OIDC_ISSUER")
	}
	if cfg.OIDC.ClientID == "" {
		cfg.OIDC.ClientID = os.Getenv("OIDC_CLIENT_ID")
	}
	if cfg.OIDC.ClientSecret == "" {
		cfg.OIDC.ClientSecret = os.Getenv("OIDC_CLIENT_SECRET")
	}
	if cfg.OIDC.RedirectURI == "" {
		cfg.OIDC.RedirectURI = os.Getenv("OIDC_REDIRECT_URI")
	}

	if cfg.OIDC.Issuer == "" || cfg.OIDC.ClientID == "" || cfg.OIDC.RedirectURI == "" {
		return nil, fmt.Errorf("oidc.issuer, oidc.client_id and oidc.redirect_uri must be set (config.json or env)")
	}
	if cfg.OIDC.CookieName == "" {
		cfg.OIDC.CookieName = "arca27_session"
	}
	if len(cfg.OIDC.Scopes) == 0 {
		cfg.OIDC.Scopes = []string{"openid", "profile", "email", "offline_access"}
	}
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 7777
	}
	if cfg.Server.BaseURL == "" {
		cfg.Server.BaseURL = fmt.Sprintf("http://localhost:%d", cfg.Server.Port)
	}
	if cfg.Authz.ModelPath == "" {
		cfg.Authz.ModelPath = "deploy/model.conf"
	}
	if cfg.Authz.PolicyPath == "" {
		cfg.Authz.PolicyPath = "deploy/policy.csv"
	}
	return &cfg, nil
}
