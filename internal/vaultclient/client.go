// Package vaultclient is the BitWarden Password Manager HTTP API client
// (Subsystem 2). It talks directly to a VaultWarden/BitWarden server using the
// native /identity endpoints (prelogin, register, connect/token) and /api/sync.
// All client-side crypto is delegated to internal/vaultcrypto.
//
// Verified against a local Docker VaultWarden instance (Spike #2) by
// cross-checking with the official `bw` CLI: an account registered by this
// client is readable by `bw login`, and items created here decrypt identically
// in `bw`.
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
	"strings"

	"github.com/ac-kurniawan/wardenssh/internal/vaultcrypto"
)

// KDF type constants as used by the BitWarden /prelogin and /register endpoints.
const (
	KdfPBKDF2 = 0
	KdfArgon2 = 1
)

// Client is an unauthenticated client for prelogin/register; Login returns an
// AuthenticatedClient for sync/item operations.
type Client struct {
	BaseURL string // e.g. "http://localhost:8000" (no trailing slash)
	HTTP    *http.Client
}

// New returns a Client for the given server base URL.
func New(baseURL string) *Client {
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), HTTP: http.DefaultClient}
}

// PreloginResult is the server's KDF parameters for an email.
type PreloginResult struct {
	KDF            int `json:"kdf"`
	KDFIterations  int `json:"kdfIterations"`
	KDFMemory      int `json:"kdfMemory,omitempty"`      // Argon2 (KiB)
	KDFParallelism int `json:"kdfParallelism,omitempty"` // Argon2
}

// Prelogin asks the server for the KDF parameters for an email.
func (c *Client) Prelogin(email string) (*PreloginResult, error) {
	body, _ := json.Marshal(map[string]string{"email": email})
	resp, err := c.HTTP.Post(c.BaseURL+"/identity/accounts/prelogin", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("prelogin: status %d: %s", resp.StatusCode, string(raw))
	}
	var pr PreloginResult
	if err := json.NewDecoder(resp.Body).Decode(&pr); err != nil {
		return nil, err
	}
	return &pr, nil
}

// AccountKeys is the cryptographic material produced at registration: the
// encrypted Protected Symmetric Key + the RSA keypair (public DER base64 +
// encrypted private key). All derived from the master password + email.
type AccountKeys struct {
	Email            string
	MasterPassword   string
	MasterKey        []byte
	AuthHashB64      string // base64 of PBKDF2(password, masterKey, 1) — sent as the `password` to login
	SymmetricKey     []byte // 64 bytes (symEnc||symMac) — used to encrypt vault items at rest
	ProtectedKey     string // type-2 encrypted SymmetricKey under master-stretched enc/mac
	PublicKeyB64     string // PKIX RSA public key, base64
	EncPrivateKey    string // type-2 encrypted PKCS8 RSA private key under SymmetricKey
}

// DeriveAccountKeys derives the full per-account cryptographic material from
// email + master password using PBKDF2 with the given iteration count (the
// VaultWarden default). Generates a fresh symmetric key + RSA keypair.
func DeriveAccountKeys(email, masterPassword string, kdfIterations int) (*AccountKeys, error) {
	mk := vaultcrypto.DeriveMasterKeyPBKDF2(masterPassword, email, kdfIterations)
	authHash := vaultcrypto.DeriveAuthHash(masterPassword, mk)

	// Fresh 64-byte user symmetric key.
	symKey := make([]byte, 64)
	if _, err := rand.Read(symKey); err != nil {
		return nil, err
	}
	symEnc, symMac := symKey[:32], symKey[32:]

	// Protected Symmetric Key: encrypt symKey under the master-stretched keys.
	stretchEnc, stretchMac := vaultcrypto.StretchKeys(mk)
	protected, err := vaultcrypto.WrapSymKey(stretchEnc, stretchMac, symKey)
	if err != nil {
		return nil, err
	}

	// RSA keypair: encrypt private key under the user's symmetric key.
	pubDER, encPriv, err := vaultcrypto.GenerateRSAKeyPair(symEnc, symMac)
	if err != nil {
		return nil, err
	}

	return &AccountKeys{
		Email:          email,
		MasterPassword: masterPassword,
		MasterKey:      mk,
		AuthHashB64:    base64.StdEncoding.EncodeToString(authHash),
		SymmetricKey:   symKey,
		ProtectedKey:   protected,
		PublicKeyB64:   vaultcrypto.Base64DER(pubDER),
		EncPrivateKey:  encPriv,
	}, nil
}

// Register creates a new account on the server. Returns nil on success.
// kdfIterations is sent along so the server records the user's KDF params.
func (c *Client) Register(ak *AccountKeys, kdfIterations int) error {
	// Device id: random hex so repeated runs are distinguishable in logs.
	devID := make([]byte, 16)
	_, _ = rand.Read(devID)

	body := map[string]any{
		"email":              ak.Email,
		"masterPasswordHash": ak.AuthHashB64,
		"key":                ak.ProtectedKey,
		"keys": map[string]string{
			"publicKey":           ak.PublicKeyB64,
			"encryptedPrivateKey": ak.EncPrivateKey,
		},
		"kdf":            KdfPBKDF2,
		"kdfIterations":  kdfIterations,
		"deviceIdentifier": hex.EncodeToString(devID),
	}
	raw, _ := json.Marshal(body)
	resp, err := c.HTTP.Post(c.BaseURL+"/identity/accounts/register", "application/json", bytes.NewReader(raw))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		out, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("register: status %d: %s", resp.StatusCode, string(out))
	}
	return nil
}