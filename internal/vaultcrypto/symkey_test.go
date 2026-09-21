package vaultcrypto_test

import (
	"crypto/rand"
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

// TestUnwrapCipherKeyRoundTrip: a per-item cipher key is a 64-byte symmetric
// key wrapped under the account key (type-2). Unwrapping must recover the
// item's own enc(32)||mac(32) pair. New VaultWarden/BitWarden items carry
// this wrapped key; without unwrapping it, their fields are undecryptable
// under the account key (HMAC failure) and the item silently disappears.
func TestUnwrapCipherKeyRoundTrip(t *testing.T) {
	acctEnc := make([]byte, 32)
	acctMac := make([]byte, 32)
	_, _ = rand.Read(acctEnc)
	_, _ = rand.Read(acctMac)

	itemEnc := make([]byte, 32)
	itemMac := make([]byte, 32)
	_, _ = rand.Read(itemEnc)
	_, _ = rand.Read(itemMac)
	itemKey := append(append([]byte{}, itemEnc...), itemMac...)

	wrapped, err := vaultcrypto.Encrypt(acctEnc, acctMac, itemKey)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	gotEnc, gotMac, err := vaultcrypto.UnwrapCipherKey(acctEnc, acctMac, wrapped)
	if err != nil {
		t.Fatalf("UnwrapCipherKey: %v", err)
	}
	if string(gotEnc) != string(itemEnc) {
		t.Error("item enc key mismatch")
	}
	if string(gotMac) != string(itemMac) {
		t.Error("item mac key mismatch")
	}
}

// TestUnwrapCipherKeyRejectsWrongAccountKey: a wrapped item key must not
// unwrap under the wrong account key.
func TestUnwrapCipherKeyRejectsWrongAccountKey(t *testing.T) {
	acctEnc := make([]byte, 32)
	acctMac := make([]byte, 32)
	_, _ = rand.Read(acctEnc)
	_, _ = rand.Read(acctMac)

	itemKey := make([]byte, 64)
	_, _ = rand.Read(itemKey)
	wrapped, err := vaultcrypto.Encrypt(acctEnc, acctMac, itemKey)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	wrongMac := make([]byte, 32)
	_, _ = rand.Read(wrongMac)
	if _, _, err := vaultcrypto.UnwrapCipherKey(acctEnc, wrongMac, wrapped); err == nil {
		t.Error("UnwrapCipherKey with wrong account mac: want error, got nil")
	}
}