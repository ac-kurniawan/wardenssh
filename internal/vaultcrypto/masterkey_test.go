// Package vaultcrypto tests for master key + auth hash derivation.
//
// See masterkey.go for the function implementations and the package doc
// (cipher.go) for the full BitWarden crypto chain.
package vaultcrypto

import (
	"encoding/hex"
	"testing"
)

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