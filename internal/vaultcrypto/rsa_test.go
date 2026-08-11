package vaultcrypto_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"testing"

	"github.com/ac-kurniawan/wardenssh/internal/vaultcrypto"
)

// TestRSAEncryptPrivateKeyRoundTrip: generating an RSA keypair, encrypting the
// private key under a symmetric key, then decrypting it back yields a key that
// can still perform RSA operations (decrypt a test ciphertext). This is the
// path used at registration and at login (decrypting the stored private key).
func TestRSAEncryptPrivateKeyRoundTrip(t *testing.T) {
	symEnc := make([]byte, 32)
	symMac := make([]byte, 32)
	_, _ = rand.Read(symEnc)
	_, _ = rand.Read(symMac)

	pubDER, encPriv, err := vaultcrypto.GenerateRSAKeyPair(symEnc, symMac)
	if err != nil {
		t.Fatalf("GenerateRSAKeyPair: %v", err)
	}
	if len(pubDER) == 0 || encPriv == "" {
		t.Fatal("GenerateRSAKeyPair returned empty values")
	}

	// Round-trip the private key through the encrypted form.
	rsaPriv, err := vaultcrypto.DecryptPrivateKey(symEnc, symMac, encPriv)
	if err != nil {
		t.Fatalf("DecryptPrivateKey: %v", err)
	}
	_ = rsaPriv // already *rsa.PrivateKey; functional check below proves usability

	// Functional check: encrypt with the public key, decrypt with the recovered
	// private key. Proves the round-tripped key is usable, not just parseable.
	msg := []byte("rsa-round-trip")
	ct, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, &rsaPriv.PublicKey, msg, nil)
	if err != nil {
		t.Fatalf("EncryptOAEP: %v", err)
	}
	pt, err := rsa.DecryptOAEP(sha256.New(), rand.Reader, rsaPriv, ct, nil)
	if err != nil {
		t.Fatalf("DecryptOAEP: %v", err)
	}
	if string(pt) != string(msg) {
		t.Errorf("RSA round-trip = %q, want %q", pt, msg)
	}

	// Public key must match the DER we returned (re-parse and compare modulus).
	pub, err := vaultcrypto.ParseRSAPublicKey(pubDER)
	if err != nil {
		t.Fatalf("ParseRSAPublicKey: %v", err)
	}
	if pub.N.Cmp(rsaPriv.N) != 0 || pub.E != rsaPriv.E {
		t.Error("returned public DER does not match the generated keypair")
	}
}

// TestDecryptPrivateKeyRejectsBadMAC: a tampered encrypted-private-key string
// must fail (the private key is high-value — never decrypt without MAC check).
func TestDecryptPrivateKeyRejectsBadMAC(t *testing.T) {
	symEnc, _ := keyPair(t)
	_, encPriv, _ := vaultcrypto.GenerateRSAKeyPair(symEnc, make([]byte, 32))

	otherMac := make([]byte, 32)
	_, _ = rand.Read(otherMac)
	if _, err := vaultcrypto.DecryptPrivateKey(symEnc, otherMac, encPriv); err == nil {
		t.Error("DecryptPrivateKey with wrong macKey: want error, got nil")
	}
}