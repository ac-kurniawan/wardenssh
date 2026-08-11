// Package vaultcrypto: symkey.go derives the stretched keys (HKDF) from a
// master key and wraps/unwraps the user's Protected Symmetric Key (the 64-byte
// symmetric key returned by /identity/connect/token, encrypted under the
// master-stretched enc/mac keys).
package vaultcrypto

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"golang.org/x/crypto/hkdf"
)

// StretchKeys derives the (encKey, macKey) pair from a master key via
// HKDF-SHA256 with the standard BitWarden info literals "enc" and "mac" and
// a zero (32-byte) salt. encKey/macKey are 32 bytes each.
func StretchKeys(masterKey []byte) (encKey, macKey []byte) {
	return hkdfSha256(masterKey, []byte("enc"), 32), hkdfSha256(masterKey, []byte("mac"), 32)
}

// hkdfSha256 expands a master PRK into `length` bytes using HKDF-Expand
// (RFC 5869 §2.3 only — NO extract step). BitWarden's hkdfExpand uses this
// exact variant: the master key is used directly as the PRK, with no salt.
func hkdfSha256(prk, info []byte, length int) []byte {
	r := hkdf.Expand(sha256.New, prk, info)
	out := make([]byte, length)
	_, _ = r.Read(out)
	return out
}

// WrapSymKey encrypts a 64-byte symmetric key (symEnc||symMac) under the given
// stretched enc/mac keys and returns a BitWarden type-2 protected-key string.
// (Used at registration: the server stores this as the user's Protected Key.)
func WrapSymKey(encKey, macKey, symKey []byte) (string, error) {
	if len(symKey) != 64 {
		return "", errors.New("vaultcrypto: symKey must be 64 bytes (symEnc||symMac)")
	}
	return Encrypt(encKey, macKey, symKey)
}

// UnwrapSymKey decrypts a protected-key string (from /identity/connect/token
// "Key" field) into the 64-byte symmetric key (symEnc(32)||symMac(32)).
func UnwrapSymKey(encKey, macKey []byte, protected string) ([]byte, error) {
	out, err := Decrypt(encKey, macKey, protected)
	if err != nil {
		return nil, err
	}
	if len(out) != 64 {
		return nil, errors.New("vaultcrypto: unwrapped symKey not 64 bytes (legacy key?)")
	}
	return out, nil
}

// keep import used for the byte-order helper below if needed; harmless stub.
var _ = binary.LittleEndian
var _ = hmac.New