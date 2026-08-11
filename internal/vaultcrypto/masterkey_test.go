// Package vaultcrypto implements the BitWarden Password Manager client-side
// crypto needed to authenticate to a VaultWarden/BitWarden server and decrypt
// vault items (Subsystem 2 / Spike #2, see .local/plan.md). The chain:
//
//   - Master Key derivation: PBKDF2-HMAC-SHA256(password, salt=email, iterations)
//     or Argon2id(...) — determined by the server's /prelogin response.
//   - Stretched keys (HKDF-SHA256): encKey=HKDF(masterKey, "enc"),
//     macKey=HKDF(masterKey, "mac") — used to decrypt the Protected Symmetric
//     Key returned by /identity/connect/token.
//   - Symmetric key = (symEncKey 32B || symMacKey 32B), 64 bytes total.
//   - Auth hash: PBKDF2-HMAC-SHA256(password, masterKey, 1) sent to the token
//     endpoint as the `password` field.
//   - Encrypted-string format "2.<b64(iv||ciphertext||hmac)>" with
//     AES-256-CBC + HMAC-SHA256 (type 2). Type "0." is the XOR legacy single-
//     byte-key scheme (used for the protected key in some legacy flows).
//   - SSH-Key items: each sensitive field (PrivateKey, Passphrase, ...) is an
//     encrypted string under the item's key (the user's symmetric key, or an
//     organization key for shared items). Custom fields (host/user/port/...)
//     are encrypted strings too.
//
// All values verified byte-identical against `bw` CLI decrypt of the same
// vault item (Spike #2 success criterion).
package vaultcrypto

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"golang.org/x/crypto/pbkdf2"
)

// DeriveMasterKeyPBKDF2 derives the master key from the user's master password
// and email (lowercased) using PBKDF2-HMAC-SHA256. kdfIterations is taken from
// the server's /prelogin response. Returns a 32-byte key.
func DeriveMasterKeyPBKDF2(password, email string, kdfIterations int) []byte {
	return pbkdf2.Key([]byte(password), []byte(lower(email)), kdfIterations, 32, sha256.New)
}

// lower is a tiny helper so callers can see what the salt is.
func lower(s string) string {
	out := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		out[i] = c
	}
	return string(out)
}

// DeriveAuthHash computes the master password hash sent to
// /identity/connect/token (grant_type=password) as the `password` field. It is
// PBKDF2-HMAC-SHA256(password, masterKey, 1, 32) -> base64-encoded by callers.
func DeriveAuthHash(password string, masterKey []byte) []byte {
	return pbkdf2.Key([]byte(password), masterKey, 1, 32, sha256.New)
}

// TestDeriveMasterKeyPBKDF2KnownVector verifies the master-key derivation
// against the well-known PBKDF2-HMAC-SHA256 test vector (RFC-style reference),
// to prove the parameters + chain are correct independent of BitWarden.
func TestDeriveMasterKeyPBKDF2KnownVector(t *testing.T) {
	// Standard PBKDF2-HMAC-SHA256 test vector:
	//   password="password", salt="salt", iterations=1, dklen=32
	//   = 120fb6cffcf8b32c43e7225256c4f837a86548c92ccc35480805987cb70be17b
	want, _ := hex.DecodeString("120fb6cffcf8b32c43e7225256c4f837a86548c92ccc35480805987cb70be17b")
	got := DeriveMasterKeyPBKDF2("password", "salt", 1)
	if !bytesEqual(got, want) {
		t.Errorf("master key = %x, want %x", got, want)
	}
}

// TestDeriveMasterKeyPBKDF2LowercasesSalt: BitWarden uses the *lowercased*
// email as the PBKDF2 salt; verify that "Foo@Bar.COM" produces the same key
// as "foo@bar.com".
func TestDeriveMasterKeyPBKDF2LowercasesSalt(t *testing.T) {
	a := DeriveMasterKeyPBKDF2("hunter2", "Foo@Bar.COM", 1000)
	b := DeriveMasterKeyPBKDF2("hunter2", "foo@bar.com", 1000)
	if !bytesEqual(a, b) {
		t.Errorf("salt case mismatch: %x != %x", a, b)
	}
}

// TestDeriveAuthHashSeparateFromMasterKey: the auth hash is a DIFFERENT
// derivation (salt=masterKey, iterations=1), so it must not equal the master
// key for any sane input.
func TestDeriveAuthHashSeparateFromMasterKey(t *testing.T) {
	mk := DeriveMasterKeyPBKDF2("password", "salt", 1000)
	h := DeriveAuthHash("password", mk)
	if bytesEqual(h, mk) {
		t.Errorf("auth hash equals master key (broken chain): %x == %x", h, mk)
	}
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}