// Package vaultcrypto: cipher.go implements the BitWarden encrypted-string
// format "2.<base64(iv||ciphertext||mac)>" with AES-256-CBC + HMAC-SHA256
// (encType=2). Used for vault item fields, custom fields, the Protected
// Symmetric Key, and the encrypted RSA private key. Both directions are
// implemented: Encrypt is needed to register an account + store items;
// Decrypt is what WardenSSH uses at runtime to consume the vault.
package vaultcrypto

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
)

// Encrypt produces a BitWarden type-2 encrypted string from plaintext under
// the given (encKey=32 bytes, macKey=32 bytes). Returns "2.<b64(iv||ct||mac)>".
func Encrypt(encKey, macKey, plain []byte) (string, error) {
	if len(encKey) != 32 {
		return "", errors.New("vaultcrypto: encKey must be 32 bytes")
	}
	if len(macKey) != 32 {
		return "", errors.New("vaultcrypto: macKey must be 32 bytes")
	}
	block, err := aes.NewCipher(encKey)
	if err != nil {
		return "", err
	}
	iv := make([]byte, aes.BlockSize)
	if _, err := rand.Read(iv); err != nil {
		return "", err
	}
	padded := pkcs7Pad(plain, aes.BlockSize)
	ct := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(ct, padded)

	mac := hmac.New(sha256.New, macKey)
	mac.Write(iv)
	mac.Write(ct)
	tag := mac.Sum(nil)

	blob := make([]byte, 0, len(iv)+len(ct)+len(tag))
	blob = append(blob, iv...)
	blob = append(blob, ct...)
	blob = append(blob, tag...)
	return "2." + base64.StdEncoding.EncodeToString(blob), nil
}

// Decrypt parses a BitWarden encrypted string ("2.<b64>") and returns the
// plaintext under the given (encKey=32 bytes, macKey=32 bytes). HMAC is
// verified before AES decrypt; a bad MAC returns an error.
func Decrypt(encKey, macKey []byte, enc string) ([]byte, error) {
	if len(encKey) != 32 {
		return nil, errors.New("vaultcrypto: encKey must be 32 bytes")
	}
	if len(macKey) != 32 {
		return nil, errors.New("vaultcrypto: macKey must be 32 bytes")
	}
	b64 := enc
	if strings.HasPrefix(b64, "2.") {
		b64 = b64[2:]
	} else if strings.HasPrefix(b64, "0.") {
		// Legacy XOR scheme — not used for AES-protected values in
		// VaultWarden; refuse explicitly so a wrong-format value is loud.
		return nil, errors.New("vaultcrypto: legacy type-0 (XOR) not supported by Decrypt")
	}
	blob, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, errors.New("vaultcrypto: bad base64: " + err.Error())
	}
	if len(blob) < aes.BlockSize+32 {
		return nil, errors.New("vaultcrypto: ciphertext too short")
	}
	iv := blob[:aes.BlockSize]
	tag := blob[len(blob)-32:]
	ct := blob[aes.BlockSize : len(blob)-32]

	mac := hmac.New(sha256.New, macKey)
	mac.Write(iv)
	mac.Write(ct)
	if !hmac.Equal(mac.Sum(nil), tag) {
		return nil, errors.New("vaultcrypto: HMAC verification failed")
	}

	block, err := aes.NewCipher(encKey)
	if err != nil {
		return nil, err
	}
	padded := make([]byte, len(ct))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(padded, ct)
	return pkcs7Unpad(padded)
}

// pkcs7Pad appends PKCS7 padding to make the plaintext a multiple of blockSize.
func pkcs7Pad(in []byte, blockSize int) []byte {
	pad := blockSize - (len(in) % blockSize)
	out := make([]byte, len(in)+pad)
	copy(out, in)
	for i := len(in); i < len(out); i++ {
		out[i] = byte(pad)
	}
	return out
}

// pkcs7Unpad removes PKCS7 padding; returns an error on malformed padding.
func pkcs7Unpad(in []byte) ([]byte, error) {
	if len(in) == 0 {
		return nil, errors.New("vaultcrypto: empty padding input")
	}
	pad := int(in[len(in)-1])
	if pad == 0 || pad > len(in) {
		return nil, errors.New("vaultcrypto: invalid PKCS7 padding")
	}
	for i := len(in) - pad; i < len(in); i++ {
		if int(in[i]) != pad {
			return nil, errors.New("vaultcrypto: invalid PKCS7 padding bytes")
		}
	}
	return in[:len(in)-pad], nil
}

// Compile-time guard to surface dangling usage cleanly.
var _ = bytes.Equal