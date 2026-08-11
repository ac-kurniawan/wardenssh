package session_test

import (
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ac-kurniawan/wardenssh/internal/session"
)

// --- cross-platform dummy commands ---

func echoArgs(marker string) []string {
	if runtime.GOOS == "windows" {
		return []string{"cmd", "/c", "echo " + marker}
	}
	return []string{"echo", marker}
}

func sleepArgs(secs int) []string {
	if runtime.GOOS == "windows" {
		// ping waits ~secs seconds and is universally available on Windows.
		return []string{"ping", "-n", itoa(secs + 1), "127.0.0.1", "-w", "1000"}
	}
	return []string{"sleep", itoa(secs)}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	if n < 0 {
		return "0"
	}
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// --- ring buffer ---

// TestRingKeepsLastBytes: writes beyond capacity keep only the trailing cap
// bytes, dropping the oldest (Q18/iii bounded memory for background sessions).
func TestRingKeepsLastBytes(t *testing.T) {
	r := session.NewRing(8)
	r.Write([]byte("0123456789ABCDEF")) // 16 bytes into cap=8
	if got := r.Bytes(); string(got) != "89ABCDEF" {
		t.Errorf("Bytes = %q, want last 8 of \"0123456789ABCDEF\" (\"89ABCDEF\")", string(got))
	}
}

// TestRingGrowsWithinCap: writes under capacity accumulate in order.
func TestRingGrowsWithinCap(t *testing.T) {
	r := session.NewRing(64)
	r.Write([]byte("hello"))
	r.Write([]byte(" world"))
	if got := strings.TrimSpace(string(r.Bytes())); got != "hello world" {
		t.Errorf("Bytes = %q, want \"hello world\"", got)
	}
}

// --- session: real PTY against a dummy echo command ---

// TestSessionCapturesChildOutput: a started session captures the child's
// stdout/stderr through the PTY into its ring buffer, and the session exits
// cleanly on its own (no Kill needed).
func TestSessionCapturesChildOutput(t *testing.T) {
	marker := "WARDENSSH-MARKER-1234"
	s, err := session.Start("s1", "prod-db-01", "file", echoArgs(marker))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Kill() // safety net so a failure can't leak a process

	select {
	case <-s.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("session did not exit within 5s")
	}
	<-s.Done()
	if !strings.Contains(string(s.Buffer()), marker) {
		t.Errorf("buffer = %q, want it to contain %q", s.Buffer(), marker)
	}
}

// TestSessionKillTerminates: a long-running session is terminated by Kill and
// reports exited promptly (the agent process must control session lifetime).
func TestSessionKillTerminates(t *testing.T) {
	s, err := session.Start("s2", "web-02", "file", sleepArgs(30))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := s.Kill(); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	select {
	case <-s.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("Kill did not cause session to exit within 3s")
	}
}

// --- manager: N concurrent sessions, active, kill-all ---

// TestManagerSpawnActiveKillAll: multiple concurrent sessions are tracked in
// order; the last spawned becomes active; KillAll empties the live set.
func TestManagerSpawnActiveKillAll(t *testing.T) {
	m := session.NewManager()
	m.Spawn("a", "file", echoArgs("A"))
	m.Spawn("b", "file", echoArgs("B"))
	m.Spawn("c", "vw:personal", echoArgs("C"))

	if got := len(m.Sessions()); got != 3 {
		t.Errorf("Sessions() = %d, want 3", got)
	}
	if m.Active() == nil || m.Active().Alias() != "c" {
		t.Errorf("Active = %+v, want last spawned (alias c)", m.Active())
	}
	// (We do NOT assert Live==3 here: the short echo commands may exit within
	// milliseconds of Spawn. The deterministic check is post-KillAll below.)
	m.KillAll()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if len(m.Live()) == 0 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Errorf("after KillAll, Live = %d, want 0", len(m.Live()))
}

// TestManagerSetActiveSwitches: SetActive changes the foreground session
// (Q18/iii yield-and-switch: only one session is "active" at a time).
func TestManagerSetActiveSwitches(t *testing.T) {
	m := session.NewManager()
	a, _ := m.Spawn("a", "file", echoArgs("A"))
	b, _ := m.Spawn("b", "file", echoArgs("B"))
	if m.Active().ID() != b.ID() {
		t.Errorf("Active = %s, want %s (last spawned)", m.Active().ID(), b.ID())
	}
	m.SetActive(a.ID())
	if m.Active().ID() != a.ID() {
		t.Errorf("after SetActive(a), Active = %s, want %s", m.Active().ID(), a.ID())
	}
	if got := len(m.Sessions()); got != 2 {
		t.Errorf("Sessions = %d, want 2", got)
	}
}