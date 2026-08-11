// Package vaultclient: auth.go implements the password-grant login flow and
// the vault sync, decrypting the user's Protected Symmetric Key + RSA private
// key from the token response. This is the runtime entry point WardenSSH uses
// to authenticate to a vault and pull the encrypted item list.
package vaultclient

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/ac-kurniawan/wardenssh/internal/vaultcrypto"
)

// TokenResponse is the subset of /identity/connect/token's JSON we need.
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
	Key          string `json:"Key"`         // Protected Symmetric Key (type-2 encrypted, under master-stretched)
	PrivateKey   string `json:"PrivateKey"`  // Encrypted RSA private key (type-2, under user sym key)
}

// Session is an authenticated vault session: the access token + the decrypted
// user symmetric key (used to decrypt vault items) + the RSA private key.
type Session struct {
	AccessToken string
	SymEnc      []byte // 32 bytes — AES key for vault items
	SymMac      []byte // 32 bytes — HMAC key for vault items
	PrivateKey  string // encrypted RSA private key (decrypted lazily by callers via vaultcrypto.DecryptPrivateKey)
}

// Login authenticates with email + master password using the PBKDF2 KDF path.
// masterPassword is the user's master password (never stored; used only to
// derive the master key + auth hash in memory).
func (c *Client) Login(email, masterPassword string) (*Session, error) {
	pre, err := c.Prelogin(email)
	if err != nil {
		return nil, fmt.Errorf("prelogin: %w", err)
	}
	if pre.KDF != KdfPBKDF2 {
		return nil, fmt.Errorf("vaultclient: KDF type %d (Argon2) not yet implemented; only PBKDF2 supported", pre.KDF)
	}

	mk := vaultcrypto.DeriveMasterKeyPBKDF2(masterPassword, email, pre.KDFIterations)
	authHash := vaultcrypto.DeriveAuthHash(masterPassword, mk)

	devID := make([]byte, 16)
	_, _ = rand.Read(devID)
	form := url.Values{
		"grant_type":        {"password"},
		"username":          {email},
		"password":          {base64.StdEncoding.EncodeToString(authHash)},
		"scope":             {"api offline_access"},
		"client_id":         {"cli"},
		"client_secret":     {"na"},
		"deviceType":        {"2"},
		"deviceIdentifier":  {hex.EncodeToString(devID)},
		"deviceName":        {"wardenssh"},
	}
	req, _ := http.NewRequest(http.MethodPost, c.BaseURL+"/identity/connect/token", bytes.NewBufferString(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("login: status %d: %s", resp.StatusCode, string(raw))
	}

	var tr TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return nil, fmt.Errorf("login: decode token: %w", err)
	}

	// Decrypt the Protected Symmetric Key. The format depends on the account's
	// age: type "0." (AesCbc256_B64) uses the raw master key; type "2."
	// (AesCbc256_HmacSha256_B64) uses the HKDF-stretched master key.
	var symKey []byte
	if len(tr.Key) > 0 && tr.Key[0] == '0' {
		// Legacy: decrypt with raw master key (no MAC, type 0).
		dec, err := vaultcrypto.Decrypt(mk, nil, tr.Key)
		if err != nil {
			return nil, fmt.Errorf("login: unwrap protected key (type 0): %w", err)
		}
		symKey = dec
	} else {
		// Modern: decrypt with HKDF-stretched master key (type 2).
		stretchEnc, stretchMac := vaultcrypto.StretchKeys(mk)
		dec, err := vaultcrypto.UnwrapSymKey(stretchEnc, stretchMac, tr.Key)
		if err != nil {
			return nil, fmt.Errorf("login: unwrap protected key (type 2): %w", err)
		}
		symKey = dec
	}

	return &Session{
		AccessToken: tr.AccessToken,
		SymEnc:      symKey[:32],
		SymMac:      symKey[32:],
		PrivateKey:  tr.PrivateKey,
	}, nil
}

// LoginWith2FA authenticates with email, master password, and a 2FA code (TOTP/Email).
func (c *Client) LoginWith2FA(email, masterPassword, twoFactorCode string, provider int) (*Session, error) {
	pre, err := c.Prelogin(email)
	if err != nil {
		return nil, fmt.Errorf("prelogin: %w", err)
	}
	if pre.KDF != KdfPBKDF2 {
		return nil, fmt.Errorf("vaultclient: KDF type %d (Argon2) not yet implemented; only PBKDF2 supported", pre.KDF)
	}

	mk := vaultcrypto.DeriveMasterKeyPBKDF2(masterPassword, email, pre.KDFIterations)
	authHash := vaultcrypto.DeriveAuthHash(masterPassword, mk)

	devID := make([]byte, 16)
	_, _ = rand.Read(devID)
	form := url.Values{
		"grant_type":        {"password"},
		"username":          {email},
		"password":          {base64.StdEncoding.EncodeToString(authHash)},
		"twoFactorToken":    {twoFactorCode},
		"twoFactorProvider": {fmt.Sprintf("%d", provider)},
		"twoFactorRemember": {"1"},
		"scope":             {"api offline_access"},
		"client_id":         {"cli"},
		"client_secret":     {"na"},
		"deviceType":        {"2"},
		"deviceIdentifier":  {hex.EncodeToString(devID)},
		"deviceName":        {"wardenssh"},
	}
	req, _ := http.NewRequest(http.MethodPost, c.BaseURL+"/identity/connect/token", bytes.NewBufferString(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("login 2fa: status %d: %s", resp.StatusCode, string(raw))
	}

	var tr TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return nil, fmt.Errorf("login 2fa: decode token: %w", err)
	}

	var symKey []byte
	if len(tr.Key) > 0 && tr.Key[0] == '0' {
		dec, err := vaultcrypto.Decrypt(mk, nil, tr.Key)
		if err != nil {
			return nil, fmt.Errorf("login 2fa: unwrap protected key (type 0): %w", err)
		}
		symKey = dec
	} else {
		stretchEnc, stretchMac := vaultcrypto.StretchKeys(mk)
		dec, err := vaultcrypto.UnwrapSymKey(stretchEnc, stretchMac, tr.Key)
		if err != nil {
			return nil, fmt.Errorf("login 2fa: unwrap protected key (type 2): %w", err)
		}
		symKey = dec
	}

	return &Session{
		AccessToken: tr.AccessToken,
		SymEnc:      symKey[:32],
		SymMac:      symKey[32:],
		PrivateKey:  tr.PrivateKey,
	}, nil
}

// RefreshLogin authenticates using a refresh token obtained from a previous Login.
func (c *Client) RefreshLogin(email, refreshToken string) (*Session, error) {
	devID := make([]byte, 16)
	_, _ = rand.Read(devID)
	form := url.Values{
		"grant_type":        {"refresh_token"},
		"refresh_token":     {refreshToken},
		"client_id":         {"cli"},
		"client_secret":     {"na"},
		"deviceType":        {"2"},
		"deviceIdentifier":  {hex.EncodeToString(devID)},
		"deviceName":        {"wardenssh"},
	}
	req, _ := http.NewRequest(http.MethodPost, c.BaseURL+"/identity/connect/token", bytes.NewBufferString(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("refresh login: status %d: %s", resp.StatusCode, string(raw))
	}

	var tr TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return nil, fmt.Errorf("refresh login: decode token: %w", err)
	}

	// Decrypt the Protected Symmetric Key if returned in refresh
	if len(tr.Key) > 0 {
		// Note: Refresh responses might require stored keys or full login if key is empty.
		// For BitWarden API, if Key is provided in refresh token response:
		if tr.Key[0] == '0' {
			return nil, fmt.Errorf("refresh login: legacy key format not supported on refresh")
		}
	}

	return &Session{
		AccessToken: tr.AccessToken,
		PrivateKey:  tr.PrivateKey,
	}, nil
}

// SyncResponse is the subset of /api/sync's JSON we consume. The full sync
// payload is large; we only decode the Cipher (item) list.
type SyncResponse struct {
	Profile struct {
		ID    string `json:"Id"`
		Email string `json:"Email"`
	} `json:"Profile"`
	Ciphers []Cipher `json:"Ciphers"`
}

// Cipher is a vault item as returned by /api/sync. SSH-Key items have
// Type==4 (SshKey) in newer BitWarden; login items are Type==1.
type Cipher struct {
	ID     string `json:"Id"`
	Name   string `json:"Name"`   // encrypted string
	Type   int    `json:"Type"`   // 1=Login, 2=SecureNote, 3=Card, 4=Identity, 1000=SshKey (BitWarden SDK uses 4; VaultWarden may differ)
	Notes  string `json:"Notes"`  // encrypted, optional
	SshKey *struct {
		PrivateKey  string `json:"PrivateKey"`  // encrypted string
		PublicKey   string `json:"PublicKey"`   // encrypted string
		KeyFingerprint string `json:"KeyFingerprint"` // encrypted string
		Passphrase  string `json:"Passphrase"`  // encrypted string, optional
	} `json:"SshKey,omitempty"`
	Fields []CustomField `json:"Fields,omitempty"`
}

// CustomField is a name/value pair on a cipher (host/user/port/proxyjump live here).
type CustomField struct {
	Name  string `json:"Name"`  // encrypted string
	Value string `json:"Value"` // encrypted string
	Type  int    `json:"Type"`  // 0=Text, 1=Hidden, 2=Boolean
}

// Sync fetches the full vault. Requires an authenticated Session.
func (c *Client) Sync(s *Session) (*SyncResponse, error) {
	req, _ := http.NewRequest(http.MethodGet, c.BaseURL+"/api/sync", nil)
	req.Header.Set("Authorization", "Bearer "+s.AccessToken)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("sync: status %d: %s", resp.StatusCode, string(raw))
	}
	var sr SyncResponse
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		return nil, fmt.Errorf("sync: decode: %w", err)
	}
	return &sr, nil
}

// DecryptField decrypts a BitWarden encrypted string under the session's
// symmetric key. Convenience wrapper around vaultcrypto.Decrypt.
func (s *Session) DecryptField(enc string) ([]byte, error) {
	return vaultcrypto.Decrypt(s.SymEnc, s.SymMac, enc)
}