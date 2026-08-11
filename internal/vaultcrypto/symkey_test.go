package vaultcrypto_test

import (
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/ac-kurniawan/wardenssh/internal/vaultcrypto"
)

// TestStretchedKeysDeterministic: HKDF stretch of the same master key must be
// reproducible — encKey and macKey don't depend on any randomness.
func TestStretchedKeysDeterministic(t *testing.T) {
	mk := vaultcrypto.DeriveMasterKeyPBKDF2("password", "salt", 100)
	enc1, mac1 := vaultcrypto.StretchKeys(mk)
	enc2, mac2 := vaultcrypto.StretchKeys(mk)
	if string(enc1) != string(enc2) {
		t.Errorf("encKey not deterministic: %x != %x", enc1, enc2)
	}
	if string(mac1) != string(mac2) {
		t.Errorf("macKey not deterministic: %x != %x", mac1, mac2)
	}
	if string(enc1) == string(mac1) {
		t.Error("encKey == macKey (HKDF 'info' ignored)")
	}
}

// TestUnwrapProtectedSymKeyRoundTrip: producing a protected-symmetric-key
// blob exactly the way BitWarden does (encrypt the 64-byte symKey under the
// master-stretched enc/mac) and then unwrapping it recovers the original
// 64-byte symKey (split into enc + mac halves).
func TestUnwrapProtectedSymKeyRoundTrip(t *testing.T) {
	mk := vaultcrypto.DeriveMasterKeyPBKDF2("password", "salt", 1000)
	encKey, macKey := vaultcrypto.StretchKeys(mk)

	// Build a 64-byte user symmetric key (32 enc + 32 mac).
	symEnc := make([]byte, 32)
	symMac := make([]byte, 32)
	_, _ = rand.Read(symEnc)
	_, _ = rand.Read(symMac)
	symKey := append(append([]byte{}, symEnc...), symMac...)

	protected, err := vaultcrypto.WrapSymKey(encKey, macKey, symKey)
	if err != nil {
		t.Fatalf("WrapSymKey: %v", err)
	}
	if !strings.HasPrefix(protected, "2.") {
		t.Errorf("protected key format: %q, want '2.' prefix", protected)
	}

	got, err := vaultcrypto.UnwrapSymKey(encKey, macKey, protected)
	if err != nil {
		t.Fatalf("UnwrapSymKey: %v", err)
	}
	if len(got) != 64 {
		t.Fatalf("unwrapped symKey len = %d, want 64", len(got))
	}
	if string(got[:32]) != string(symEnc) {
		t.Errorf("symEnc mismatch")
	}
	if string(got[32:]) != string(symMac) {
		t.Errorf("symMac mismatch")
	}

	// The base64 blob size: iv(16) + ct(64+pad to 80) + mac(32) = 128 bytes.
	raw, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(protected, "2."))
	if len(raw) != 16+80+32 {
		t.Errorf("protected blob bytes = %d, want 128", len(raw))
	}
}

// TestUnwrapRejectsBadMAC: a tampered protected key must fail.
func TestUnwrapRejectsBadMAC(t *testing.T) {
	mk := vaultcrypto.DeriveMasterKeyPBKDF2("p", "s", 10)
	enc, mac := vaultcrypto.StretchKeys(mk)
	sym := make([]byte, 64)
	_, _ = rand.Read(sym)
	protected, _ := vaultcrypto.WrapSymKey(enc, mac, sym)

	wrongMac := make([]byte, 32)
	_, _ = rand.Read(wrongMac)
	if _, err := vaultcrypto.UnwrapSymKey(enc, wrongMac, protected); err == nil {
		t.Error("UnwrapSymKey with wrong macKey: want error, got nil")
	}
}