//go:build !windows

package sshagent

import (
	"net"
	"os"
)

// Listen binds a unix-domain socket listener at addr (the standard
// SSH_AUTH_SOCK transport on Linux/macOS). Any stale socket file at addr is
// removed so a previous crash does not block re-bind.
func Listen(addr string) (net.Listener, error) {
	_ = os.Remove(addr)
	return net.Listen("unix", addr)
}

// Dial connects to a unix-domain socket agent at addr.
func Dial(addr string) (net.Conn, error) {
	return net.Dial("unix", addr)
}