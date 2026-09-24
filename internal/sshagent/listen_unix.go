//go:build !windows

package sshagent

import (
	"fmt"
	"net"
	"os"
)

// Listen binds a unix-domain socket listener at addr (the standard
// SSH_AUTH_SOCK transport on Linux/macOS). A leftover socket from a dead
// process is removed so a crash does not block re-bind. A socket another
// live process is serving is left alone.
func Listen(addr string) (net.Listener, error) {
	if err := reclaimStaleSocket(addr); err != nil {
		return nil, err
	}
	ln, err := net.Listen("unix", addr)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(addr, 0o600); err != nil {
		_ = ln.Close()
		return nil, err
	}
	return ln, nil
}

// reclaimStaleSocket removes addr only when nothing is accepting on it.
// A live listener must not be unlinked: that would let a second process
// steal SSH_AUTH_SOCK out from under the first.
func reclaimStaleSocket(addr string) error {
	if _, err := os.Stat(addr); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	conn, err := net.Dial("unix", addr)
	if err != nil {
		return os.Remove(addr)
	}
	_ = conn.Close()
	return fmt.Errorf("sshagent: %s is already served", addr)
}

// Dial connects to a unix-domain socket agent at addr.
func Dial(addr string) (net.Conn, error) {
	return net.Dial("unix", addr)
}
