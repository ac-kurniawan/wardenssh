//go:build !windows

package session

import (
	"syscall"

	pty "github.com/aymanbagabas/go-pty"
)

// IsolateProcessGroup puts the PTY child in its own process group so close
// can reap ssh -J and SSH_ASKPASS descendants with one signal.
func IsolateProcessGroup(c *pty.Cmd) {
	// go-pty starts the child with Setsid, which already creates a new
	// session and process group (pgid == pid). Setting Setpgid too makes
	// setsid fail with EPERM. KillProcessTree signals that group.
	_ = c
}

// KillProcessTree signals the child's process group. The child is expected to
// be the group leader (see IsolateProcessGroup).
func KillProcessTree(c *pty.Cmd) error {
	if c == nil || c.Process == nil {
		return nil
	}
	// Negative pid targets the process group. Fall back to the process itself
	// if the group signal cannot be delivered (already reaped, or not a leader).
	err := syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
	if err != nil {
		return c.Process.Kill()
	}
	return nil
}
