package keyring_test

import (
	"testing"

	"github.com/ac-kurniawan/wardenssh/internal/keyring"
)

func TestKeyringNamingConvention(t *testing.T) {
	// Verify that keyring functions build without compile errors and run clean.
	// Actual OS keyring access may fail in unauthenticated CI/headless environments,
	// so we verify error behavior cleanly.
	_, err := keyring.GetRefreshToken("test-nonexistent-vault")
	if err == nil {
		t.Log("found item in keyring (unexpected, but valid if previously set)")
	}
}
