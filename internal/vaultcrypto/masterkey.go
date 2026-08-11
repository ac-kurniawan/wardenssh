// Package vaultcrypto: masterkey.go derives the master key and auth hash from
// the user's master password + email. These are the entry points to the whole
// BitWarden crypto chain (see package doc).
package vaultcrypto

import (
	"crypto/sha256"
	"strings"

	"golang.org/x/crypto/pbkdf2"
)

// DeriveMasterKeyPBKDF2 derives the master key from the user's master password
// and email (lowercased) using PBKDF2-HMAC-SHA256. kdfIterations is taken from
// the server's /prelogin response. Returns a 32-byte key.
func DeriveMasterKeyPBKDF2(password, email string, kdfIterations int) []byte {
	return pbkdf2.Key([]byte(password), []byte(strings.ToLower(email)), kdfIterations, 32, sha256.New)
}

// DeriveAuthHash computes the master password hash sent to
// /identity/connect/token (grant_type=password) as the `password` field.
// BitWarden's hashPassword calls pbkdf2(key.key, password) — i.e. the master
// key is the PBKDF2 *password* input and the master password string is the
// *salt*. Returns raw bytes; callers base64-encode for the wire.
func DeriveAuthHash(password string, masterKey []byte) []byte {
	return pbkdf2.Key(masterKey, []byte(password), 1, 32, sha256.New)
}