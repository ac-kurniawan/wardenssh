package auth

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// Session is the server-side session stored in the encrypted cookie jar.
type Session struct {
	AccessToken  string
	RefreshToken string
	Expiry       time.Time
	IDToken      string
	Subject      string
	LoginName    string
	DisplayName  string
	Roles        []string
}

// Manager wires the OIDC provider, OAuth2 config and token verification.
type Manager struct {
	Provider  *oidc.Provider
	Verifier  *oidc.IDTokenVerifier
	OAuth2    *oauth2.Config
	LoginName string
}

// Claims is the subset of token claims the spike consumes.
type Claims struct {
	Subject      string `json:"sub"`
	LoginName    string `json:"loginName,omitempty"`
	PreferredUsr string `json:"preferred_username,omitempty"`
	GivenName    string `json:"given_name,omitempty"`
	FamilyName   string `json:"family_name,omitempty"`

	// Zitadel emits role assertions under a namespaced claim:
	// "urn:zitadel:iam:org:project:roles" -> {"roleKey": {"orgId": "domain"}}.
	// Captured via ClaimRoles below (not a flat struct field).
	ClaimRoles map[string]map[string]string `json:"urn:zitadel:iam:org:project:roles,omitempty"`
}

// RoleKeys flattens the Zitadel role claim into a plain key list.
func (c *Claims) RoleKeys() []string {
	out := make([]string, 0, len(c.ClaimRoles))
	for k := range c.ClaimRoles {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// NewManager discovers the provider from its issuer URL (/.well-known/oidc-configuration).
func NewManager(ctx context.Context, issuer, clientID, clientSecret, redirectURI string, scopes []string) (*Manager, error) {
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery: %w", err)
	}

	// JWT access tokens are validated like ID tokens: same JWKS, but the
	// audience claim is the client id (Zitadel puts it in `aud`), so skip the
	// strict ID-token clientID check and do it manually per-claim.
	verifier := provider.Verifier(&oidc.Config{ClientID: clientID, SkipClientIDCheck: true})

	return &Manager{
		Provider: provider,
		Verifier: verifier,
		OAuth2: &oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			RedirectURL:  redirectURI,
			Endpoint:     provider.Endpoint(),
			Scopes:       scopes,
		},
	}, nil
}

// AuthCodeURL builds the authorization redirect with PKCE (S256) and state.
func (m *Manager) AuthCodeURL(state string) (url, codeVerifier string) {
	codeVerifier = randomKey(64)
	challenge := s256Challenge(codeVerifier)
	return m.OAuth2.AuthCodeURL(
		state,
		oauth2.SetAuthURLParam("code_challenge", challenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
	), codeVerifier
}

// Exchange swaps the auth code for tokens using the PKCE verifier.
func (m *Manager) Exchange(ctx context.Context, code, codeVerifier string) (*oauth2.Token, error) {
	return m.OAuth2.Exchange(ctx, code, oauth2.SetAuthURLParam("code_verifier", codeVerifier))
}

// Refresh rotates tokens with the stored refresh token.
func (m *Manager) Refresh(ctx context.Context, refreshToken string) (*oauth2.Token, error) {
	tok := &oauth2.Token{RefreshToken: refreshToken, Expiry: time.Now().Add(-time.Minute)}
	return m.OAuth2.TokenSource(ctx, tok).Token()
}

// Revoke best-effort revokes the refresh token at the revocation endpoint.
func (m *Manager) Revoke(ctx context.Context, refreshToken string) {
	if refreshToken == "" {
		return
	}
	ep := strings.TrimSuffix(m.Provider.Endpoint().TokenURL, "/token") + "/revoke"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ep, strings.NewReader(
		"token="+refreshToken+"&client_id="+m.OAuth2.ClientID))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := &http.Client{Timeout: 10 * time.Second}
	_, _ = client.Do(req)
}

// ValidateAccessToken verifies a Zitadel JWT access token against the JWKS and
// returns its claims. Signature, expiry and issuer are checked by go-oidc.
func (m *Manager) ValidateAccessToken(ctx context.Context, raw string) (*Claims, error) {
	idt, err := m.Verifier.Verify(ctx, raw)
	if err != nil {
		return nil, fmt.Errorf("token verification: %w", err)
	}
	var claims Claims
	if err := idt.Claims(&claims); err != nil {
		return nil, fmt.Errorf("token claims: %w", err)
	}
	return &claims, nil
}

// BuildSession assembles a Session from an OAuth2 token set: verifies the ID
// token (caller validates nonce), extracts the Zitadel role claim, and calls
// userinfo for identity fields Zitadel omits from ID tokens (loginName, name).
func (m *Manager) BuildSession(ctx context.Context, tok *oauth2.Token) (*Session, error) {
	rawID, ok := tok.Extra("id_token").(string)
	if !ok {
		return nil, fmt.Errorf("no id_token in token response")
	}
	idt, err := m.Verifier.Verify(ctx, rawID)
	if err != nil {
		return nil, fmt.Errorf("id token verification: %w", err)
	}
	var claims Claims
	if err := idt.Claims(&claims); err != nil {
		return nil, fmt.Errorf("id token claims: %w", err)
	}

	loginName, displayName := m.fetchUserInfo(ctx, tok.AccessToken)
	if displayName == "" {
		displayName = claims.PreferredUsr
	}
	return &Session{
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		Expiry:       tok.Expiry,
		IDToken:      rawID,
		Subject:      claims.Subject,
		LoginName:    loginName,
		DisplayName:  displayName,
		Roles:        claims.RoleKeys(),
	}, nil
}

// fetchUserInfo calls the userinfo endpoint (JWKS-verified response per OIDC);
// best-effort: empty strings on failure.
func (m *Manager) fetchUserInfo(ctx context.Context, accessToken string) (loginName, displayName string) {
	info, err := m.Provider.UserInfo(ctx, oauth2.StaticTokenSource(&oauth2.Token{AccessToken: accessToken}))
	if err != nil {
		return "", ""
	}
	var ui struct {
		LoginName  string `json:"loginName"`
		Preferred  string `json:"preferred_username"`
		GivenName  string `json:"given_name"`
		FamilyName string `json:"family_name"`
		Name       string `json:"name"`
	}
	if err := info.Claims(&ui); err != nil {
		return "", ""
	}
	display := ui.Name
	if display == "" {
		display = strings.TrimSpace(ui.GivenName + " " + ui.FamilyName)
	}
	if display == "" {
		display = ui.Preferred
	}
	return ui.LoginName, display
}

// JSONClaims renders arbitrary token claims for the /whoami diagnostics view.
func JSONClaims(raw string) (string, error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("not a JWT")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, payload, "", "  "); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func randomKey(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func s256Challenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
