// Package sshagent: this file holds the cross-platform accept loop. The
// listener/ dialer primitives are platform-specific (see listen_windows.go
// and listen_unix.go).
package sshagent

import (
	"errors"
	"net"

	"golang.org/x/crypto/ssh/agent"
)

// Serve accepts connections on l and serves the ssh-agent wire protocol
// using ag until the listener is closed. Each connection is handled in its
// own goroutine; a malformed frame closes only that connection (the
// underlying agent.ServeAgent returns an error on bad input) and the
// server keeps accepting new connections. Returns the final Accept error
// (net.ErrClosed after a clean shutdown).
func Serve(l net.Listener, ag agent.Agent) error {
	for {
		c, err := l.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		go func(conn net.Conn) {
			defer conn.Close()
			_ = agent.ServeAgent(ag, conn)
		}(c)
	}
}