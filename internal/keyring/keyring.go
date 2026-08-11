// Package keyring provides cross-platform OS keyring storage for vault refresh
// tokens (Q7/D), using zalando/go-keyring (Windows Credential Manager, macOS
// Keychain, Linux Secret Service).
package keyring

import (
	"fmt"

	zk "github.com/zalando/go-keyring"
)

const serviceName = "wardenssh"

// GetRefreshToken retrieves the stored refresh token for a vault source.
func GetRefreshToken(vaultName string) (string, error) {
	token, err := zk.Get(serviceName, "vw:"+vaultName)
	if err != nil {
		return "", fmt.Errorf("keyring get (%s): %w", vaultName, err)
	}
	return token, nil
}

// SetRefreshToken stores the refresh token for a vault source.
func SetRefreshToken(vaultName, token string) error {
	if err := zk.Set(serviceName, "vw:"+vaultName, token); err != nil {
		return fmt.Errorf("keyring set (%s): %w", vaultName, err)
	}
	return nil
}

// DeleteRefreshToken removes the refresh token for a vault source.
func DeleteRefreshToken(vaultName string) error {
	if err := zk.Delete(serviceName, "vw:"+vaultName); err != nil {
		return fmt.Errorf("keyring delete (%s): %w", vaultName, err)
	}
	return nil
}
