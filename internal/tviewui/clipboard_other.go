//go:build !windows

package tviewui

// copyToClipboard is a no-op on platforms without a CGO-free clipboard API
// (WardenSSH targets a pure-Go binary). Terminal text selection still works;
// it just cannot be placed on the OS clipboard here.
func copyToClipboard(text string) error {
	return nil
}
