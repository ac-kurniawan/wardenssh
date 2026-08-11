// Package sshauthsock is Spike #1 (see .local/plan.md): prove Windows
// OpenSSH 9.5p2 honors SSH_AUTH_SOCK pointing at a Go-served named pipe.
// This is the project-killing risk for Windows support.
package sshauthsock

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"net"

	"github.com/Microsoft/go-winio"
	"golang.org/x/crypto/ssh/agent"
)

// Server is a named-pipe ssh-agent server used by the spike.
type Server struct {
	Listener net.Listener
	Keyring  agent.Agent
	done     chan struct{}
}

// Start begins serving the ssh-agent protocol on the given Windows named
// pipe path (e.g. `\\.\pipe\wardenssh-spike`).
func Start(pipePath string) (*Server, error) {
	l, err := winio.ListenPipe(pipePath, nil)
	if err != nil {
		return nil, fmt.Errorf("listen pipe %q: %w", pipePath, err)
	}
	s := &Server{
		Listener: l,
		Keyring:  agent.NewKeyring(),
		done:     make(chan struct{}),
	}
	go s.serve()
	return s, nil
}

func (s *Server) serve() {
	for {
		c, err := s.Listener.Accept()
		if err != nil {
			close(s.done)
			return
		}
		go agent.ServeAgent(s.Keyring, c)
	}
}

// AddEd25519 generates a new ed25519 key and adds it to the keyring with the
// given comment (which appears in `ssh-add -l` output as the key comment).
func (s *Server) AddEd25519(comment string) error {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	return s.Keyring.Add(agent.AddedKey{
		PrivateKey: priv,
		Comment:    comment,
	})
}

// Close stops the server and closes the listener.
func (s *Server) Close() error {
	return s.Listener.Close()
}