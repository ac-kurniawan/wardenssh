//go:build windows

package sshagent

import (
	"net"

	"github.com/Microsoft/go-winio"
)

// Listen binds a Windows named pipe listener at addr (e.g.
// `\\.\pipe\wardenssh-agent-<pid>`). SSH_AUTH_SOCK clients (OpenSSH for
// Windows 9.5p2+) connect to the same path — validated by Spike #1.
func Listen(addr string) (net.Listener, error) {
	return winio.ListenPipe(addr, nil)
}

// Dial connects to a Windows named pipe agent at addr.
func Dial(addr string) (net.Conn, error) {
	return winio.DialPipe(addr, nil)
}