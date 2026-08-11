// Package session implements the WardenSSH session manager (Q18/iii
// yield-and-switch): N concurrent child processes each on their own PTY, one
// active (foreground) at a time, background sessions drained into bounded
// ring buffers so memory does not grow unbounded while a session is hidden.
//
// The "yield the real terminal to the active child + resume" rendering lives
// in the TUI (internal/tui); this package owns the PTY + lifecycle machinery
// and is tested against a dummy command (real ssh is wired later against an
// sshd target). Cross-platform PTY via github.com/aymanbagabas/go-pty
// (ConPTY on Windows, unix pty on *nix).
package session

import "sync"

// Ring is a fixed-capacity byte buffer that keeps the most recent writes,
// dropping the oldest when full. It is safe for concurrent Read/Write.
// (Q18/iii: background sessions are drained into a ring to bound memory.)
type Ring struct {
	mu  sync.Mutex
	buf []byte
	cap int
}

// NewRing returns a ring with the given byte capacity.
func NewRing(capacity int) *Ring {
	if capacity < 1 {
		capacity = 1
	}
	return &Ring{cap: capacity}
}

// Write appends b, evicting the oldest bytes when capacity is exceeded.
func (r *Ring) Write(b []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf = append(r.buf, b...)
	if len(r.buf) > r.cap {
		r.buf = r.buf[len(r.buf)-r.cap:]
	}
	return len(b), nil
}

// Bytes returns a copy of the current buffered bytes.
func (r *Ring) Bytes() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]byte, len(r.buf))
	copy(out, r.buf)
	return out
}