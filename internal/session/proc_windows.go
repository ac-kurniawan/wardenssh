//go:build windows

package session

import pty "github.com/aymanbagabas/go-pty"

// IsolateProcessGroup is a no-op on Windows: ConPTY job control is not a
// Unix process group, and named-pipe agent clients do not use setsid.
func IsolateProcessGroup(c *pty.Cmd) {}

// KillProcessTree kills only the direct child. Windows ssh.exe does not leave
// a Unix-style process group for this launcher to signal.
func KillProcessTree(c *pty.Cmd) error {
	if c == nil || c.Process == nil {
		return nil
	}
	return c.Process.Kill()
}
