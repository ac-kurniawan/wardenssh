package session

import (
	"errors"
	"sync"

	pty "github.com/aymanbagabas/go-pty"
)

// Session is one child process attached to a PTY, with its output drained
// into a bounded Ring buffer.
type Session struct {
	id, alias, source string
	pty                pty.Pty
	cmd                *pty.Cmd
	ring               *Ring

	mu      sync.Mutex
	exited  chan struct{}
	exitErr error
	closed  bool
}

// RingCapacity is the default per-session output buffer size (bytes). Large
// enough for typical scrollback of a short launcher session; bounded so many
// background sessions cannot balloon memory.
const RingCapacity = 1 << 16 // 64 KiB

// Start spawns a child process (argv[0] + argv[1:]) attached to a fresh PTY
// and begins draining its output into a ring buffer.
func Start(id, alias, source string, argv []string) (*Session, error) {
	return StartWithEnv(id, alias, source, argv, nil)
}

// StartWithEnv is like Start but appends env key-value pairs to the child's environment.
func StartWithEnv(id, alias, source string, argv []string, env []string) (*Session, error) {
	if len(argv) == 0 {
		return nil, errors.New("session: empty argv")
	}
	p, err := pty.New()
	if err != nil {
		return nil, err
	}
	c := p.Command(argv[0], argv[1:]...)
	if len(env) > 0 {
		c.Env = append(c.Env, env...)
	}
	if err := c.Start(); err != nil {
		_ = p.Close()
		return nil, err
	}
	s := &Session{
		id: id, alias: alias, source: source,
		pty:   p,
		cmd:   c,
		ring:  NewRing(RingCapacity),
		exited: make(chan struct{}),
	}
	go s.readLoop()
	go s.waitLoop()
	return s, nil
}

// ID returns the session id.
func (s *Session) ID() string { return s.id }

// Alias returns the host alias the session connects to.
func (s *Session) Alias() string { return s.alias }

// Source returns the originating source label.
func (s *Session) Source() string { return s.source }

// Write sends bytes to the child's PTY (stdin).
func (s *Session) Write(b []byte) (int, error) { return s.pty.Write(b) }

// Buffer returns a snapshot of the session's ring-buffered output.
func (s *Session) Buffer() []byte { return s.ring.Bytes() }

// Done returns a channel closed when the child exits.
func (s *Session) Done() <-chan struct{} { return s.exited }

// ExitErr returns the child's exit error (nil after a clean exit); valid
// only after Done is closed.
func (s *Session) ExitErr() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.exitErr
}

// Kill terminates the child process (best-effort).
func (s *Session) Kill() error {
	if s.cmd.Process != nil {
		return s.cmd.Process.Kill()
	}
	return nil
}

// readLoop drains the PTY master into the ring buffer until EOF.
func (s *Session) readLoop() {
	b := make([]byte, 4096)
	for {
		n, err := s.pty.Read(b)
		if n > 0 {
			s.ring.Write(b[:n])
		}
		if err != nil {
			return
		}
	}
}

// waitLoop waits for the child, records its exit error, and closes Done.
func (s *Session) waitLoop() {
	err := s.cmd.Wait()
	s.mu.Lock()
	s.exitErr = err
	if !s.closed {
		s.closed = true
		_ = s.pty.Close()
		close(s.exited)
	}
	s.mu.Unlock()
}