package vaultcrypto_test

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/ac-kurniawan/wardenssh/internal/vaultcrypto"
)

// TestAESCBCRoundTrip: encrypt then decrypt yields the original plaintext, and
// the ciphertext is non-trivially different (different IV, different bytes).
func TestAESCBCRoundTrip(t *testing.T) {
	encKey := make([]byte, 32)
	macKey := make([]byte, 32)
	_, _ = rand.Read(encKey)
	_, _ = rand.Read(macKey)
	plain := []byte("WARDENSSH test payload — round-trip me")

	ct, err := vaultcrypto.Encrypt(encKey, macKey, plain)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if !strings.HasPrefix(ct, "2.") {
		t.Errorf("ciphertext missing \"2.\" prefix: %q", ct)
	}
	if strings.Contains(ct, string(plain)) {
		t.Error("ciphertext contains plaintext (encryption broken)")
	}

	got, err := vaultcrypto.Decrypt(encKey, macKey, ct)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if !bytes.Equal(got, plain) {
		t.Errorf("round-trip = %q, want %q", got, plain)
	}
}

// TestDecryptRejectsBadMAC: a ciphertext with a tampered MAC byte must error,
// not return garbage plaintext (MAC must be verified before decrypt).
func TestDecryptRejectsBadMAC(t *testing.T) {
	encKey, macKey := keyPair(t)
	plain := []byte("tamper-me")
	ct, _ := vaultcrypto.Encrypt(encKey, macKey, plain)

	// Flip the last byte of the MAC (the third pipe-separated part).
	parts := strings.Split(strings.TrimPrefix(ct, "2."), "|")
	if len(parts) != 3 {
		t.Fatalf("expected 3 pipe parts, got %d", len(parts))
	}
	macBytes, err := base64.StdEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatalf("decode mac: %v", err)
	}
	macBytes[len(macBytes)-1] ^= 0x01
	parts[2] = base64.StdEncoding.EncodeToString(macBytes)
	tampered := "2." + strings.Join(parts, "|")

	if _, err := vaultcrypto.Decrypt(encKey, macKey, tampered); err == nil {
		t.Error("Decrypt with tampered MAC: want error, got nil (MAC not validated)")
	}
}

// TestDecryptRejectsWrongKeySequence: decrypt with a different macKey must
// fail MAC verification (decrypting with the wrong key must never succeed).
func TestDecryptRejectsWrongKey(t *testing.T) {
	encKey, macKey := keyPair(t)
	plain := []byte("wrong-key-rejection")
	ct, _ := vaultcrypto.Encrypt(encKey, macKey, plain)

	otherMac := make([]byte, 32)
	_, _ = rand.Read(otherMac)
	if _, err := vaultcrypto.Decrypt(encKey, otherMac, ct); err == nil {
		t.Error("Decrypt with wrong macKey: want error, got nil")
	}
}

// TestDecryptHandlesEmptyPlaintext: a zero-length plaintext round-trips cleanly
// (PKCS7 padding makes the ciphertext block predictable; the round trip still
// lands on empty plaintext).
func TestDecryptHandlesEmptyPlaintext(t *testing.T) {
	encKey, macKey := keyPair(t)
	ct, err := vaultcrypto.Encrypt(encKey, macKey, []byte{})
	if err != nil {
		t.Fatalf("Encrypt empty: %v", err)
	}
	got, err := vaultcrypto.Decrypt(encKey, macKey, ct)
	if err != nil {
		t.Fatalf("Decrypt empty: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("empty round-trip = %q, want empty", got)
	}
}

func keyPair(t *testing.T) (enc, mac []byte) {
	t.Helper()
	enc = make([]byte, 32)
	mac = make([]byte, 32)
	_, _ = rand.Read(enc)
	_, _ = rand.Read(mac)
	return
}