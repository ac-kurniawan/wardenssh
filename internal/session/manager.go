package session

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
)

// Manager tracks N concurrent sessions, one active (foreground) at a time
// (Q18/iii yield-and-switch). Spawn appends in order; the most recently
// spawned becomes active.
type Manager struct {
	mu       sync.Mutex
	sessions map[string]*Session
	order    []string
	active   string
}

// NewManager returns an empty session manager.
func NewManager() *Manager {
	return &Manager{sessions: map[string]*Session{}}
}

// Spawn starts a new session and makes it the active one. The id is generated.
func (m *Manager) Spawn(alias, source string, argv []string) (*Session, error) {
	id := newID()
	s, err := Start(id, alias, source, argv)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	m.sessions[id] = s
	m.order = append(m.order, id)
	m.active = id
	m.mu.Unlock()
	return s, nil
}

// Get returns the session by id (nil if not found).
func (m *Manager) Get(id string) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessions[id]
}

// Sessions returns all sessions in spawn order.
func (m *Manager) Sessions() []*Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*Session, 0, len(m.order))
	for _, id := range m.order {
		if s := m.sessions[id]; s != nil {
			out = append(out, s)
		}
	}
	return out
}

// Active returns the currently foreground session, or nil if none.
func (m *Manager) Active() *Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessions[m.active]
}

// SetActive makes id the foreground session. No-op if id is unknown.
func (m *Manager) SetActive(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[id]; ok {
		m.active = id
	}
}

// Live returns sessions whose child has not yet exited.
func (m *Manager) Live() []*Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*Session
	for _, id := range m.order {
		s := m.sessions[id]
		if s == nil {
			continue
		}
		select {
		case <-s.Done():
		default:
			out = append(out, s)
		}
	}
	return out
}

// Kill terminates the named session (best-effort).
func (m *Manager) Kill(id string) error {
	s := m.Get(id)
	if s == nil {
		return nil
	}
	return s.Kill()
}

// KillAll terminates every live session (Q31/C "Kill all" action).
func (m *Manager) KillAll() {
	for _, s := range m.Live() {
		_ = s.Kill()
	}
}

// newID returns a short random hex id.
func newID() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}