// Package vaultcrypto: rsa.go handles the BitWarden RSA keypair lifecycle:
// at registration a 2048-bit keypair is generated; the public key is stored as
// DER/base64, the private key is encrypted under the user's symmetric key and
// stored as a type-2 encrypted string. At login the private key is decrypted.
package vaultcrypto

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"errors"
)

// GenerateRSAKeyPair returns (publicKeyDER, encryptedPrivateKeyString) for a
// fresh 2048-bit RSA keypair. The private key is PKCS#8-encoded then encrypted
// under the caller-supplied symmetric key (symEnc/symMac). Used at registration.
func GenerateRSAKeyPair(symEnc, symMac []byte) (pubDER []byte, encPriv string, err error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, "", err
	}
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, "", err
	}
	enc, err := EncryptPrivateKey(symEnc, symMac, der)
	if err != nil {
		return nil, "", err
	}
	pubDER, err = x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		return nil, "", err
	}
	return pubDER, enc, nil
}

// EncryptPrivateKey encrypts a PKCS#8 RSA private key under the given
// symmetric key and returns the BitWarden type-2 encrypted string.
func EncryptPrivateKey(symEnc, symMac, pkcs8DER []byte) (string, error) {
	return Encrypt(symEnc, symMac, pkcs8DER)
}

// DecryptPrivateKey decrypts a BitWarden type-2 encrypted private key string
// under the given symmetric key and parses the PKCS#8 DER into an *rsa.PrivateKey.
func DecryptPrivateKey(symEnc, symMac []byte, encPriv string) (*rsa.PrivateKey, error) {
	der, err := Decrypt(symEnc, symMac, encPriv)
	if err != nil {
		return nil, err
	}
	k, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return nil, err
	}
	rk, ok := k.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("vaultcrypto: decrypted key is not RSA")
	}
	return rk, nil
}

// ParseRSAPublicKey parses a PKIX public-key DER into an *rsa.PublicKey.
func ParseRSAPublicKey(der []byte) (*rsa.PublicKey, error) {
	k, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return nil, err
	}
	rk, ok := k.(*rsa.PublicKey)
	if !ok {
		return nil, errors.New("vaultcrypto: public key is not RSA")
	}
	return rk, nil
}

// Base64DER returns the base64-encoded PKIX public key for a generated keypair.
// Convenience used in the register request body.
func Base64DER(pubDER []byte) string {
	return base64.StdEncoding.EncodeToString(pubDER)
}

// keep sha256 referenced (used by callers for OAEP label hashing in tests).
var _ = sha256.New