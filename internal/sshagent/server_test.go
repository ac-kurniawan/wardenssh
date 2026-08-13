package sshagent_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ac-kurniawan/wardenssh/internal/sshagent"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// macOSSunPathLimit is the length of sun_path in sockaddr_un on darwin
// (104 bytes on Linux it is 108; using the smaller macOS bound keeps the path
// portable to both). Unix socket bind fails with EINVAL beyond this.
const macOSSunPathLimit = 104

// addrFor returns a unique agent address for the current platform:
// Windows named pipe vs unix-domain socket.
func addrFor(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		name := strings.ToLower(strings.ReplaceAll(t.Name(), "/", "-"))
		return `\\.\pipe\wardenssh-` + name + "-" + randSuffix()
	}
	return filepath.Join(unixSocketDir(t), "agent.sock")
}

// unixSocketDir returns a SHORT temp directory for a unix socket. Using
// t.TempDir() is unsafe on macOS: its path nests under
// /var/folders/<hash>/T/<TestName><random>/001/ which pushes the socket path
// past sun_path's 104-byte limit and makes bind fail with EINVAL.
func unixSocketDir(t *testing.T) string {
	t.Helper()
	// os.TempDir() is short (e.g. /var/folders/<hash>/T); a short random
	// prefix keeps the resulting socket path well under the sun_path limit.
	dir, err := os.MkdirTemp("", "wssh-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// TestUnixSocketAddrFitsSunPathLimit: the generated unix socket path must be
// short enough for macOS's 104-byte sun_path bound. On GitHub's macOS runners
// os.TempDir() is already deep, so the socket dir must not add the test name.
func TestUnixSocketAddrFitsSunPathLimit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix socket path limit does not apply on Windows")
	}
	sock := filepath.Join(unixSocketDir(t), "agent.sock")
	if len(sock) >= macOSSunPathLimit {
		t.Fatalf("socket path %d bytes exceeds macOS sun_path limit of %d: %s", len(sock), macOSSunPathLimit, sock)
	}
}

func randSuffix() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return fmtHex(b[:])
}

func fmtHex(b []byte) string {
	const hex = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, v := range b {
		out[2*i] = hex[v>>4]
		out[2*i+1] = hex[v&0xf]
	}
	return string(out)
}

func loadEd25519(t *testing.T, ag *sshagent.Keyring, comment string) ssh.PublicKey {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519: %v", err)
	}
	pub, err := ag.Load(priv, comment, "sess-test")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return pub
}

// TestServerClientRoundTrip: start an agent server, load a key, connect a Go
// agent client over the pipe/socket, and verify List+Sign end-to-end through
// the agent wire protocol.
func TestServerClientRoundTrip(t *testing.T) {
	addr := addrFor(t)
	ag := sshagent.NewKeyring()
	pub := loadEd25519(t, ag, "rt-key")

	l, err := sshagent.Listen(addr)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })
	serveErr := make(chan error, 1)
	go func() { serveErr <- sshagent.Serve(l, ag) }()

	conn, err := sshagent.Dial(addr)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()
	cl := agent.NewClient(conn)

	keys, err := cl.List()
	if err != nil {
		t.Fatalf("client.List: %v", err)
	}
	if len(keys) != 1 || keys[0].Comment != "rt-key" {
		t.Fatalf("keys = %+v, want 1 key comment rt-key", keys)
	}

	data := []byte("round-trip sign payload")
	sig, err := cl.Sign(pub, data)
	if err != nil {
		t.Fatalf("client.Sign: %v", err)
	}
	if err := pub.Verify(data, sig); err != nil {
		t.Fatalf("signature verify: %v", err)
	}
}

// TestServerSurvivesMalformedInput: send junk incomplete frames; the server
// must not panic and must keep serving fresh connections afterward. The agent
// is a network server — input parsing must not crash the process.
func TestServerSurvivesMalformedInput(t *testing.T) {
	addr := addrFor(t)
	ag := sshagent.NewKeyring()
	loadEd25519(t, ag, "malf-key")

	l, err := sshagent.Listen(addr)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })
	go sshagent.Serve(l, ag)

	// Connection 1: write 2 bytes (incomplete length header), then close.
	c1, err := sshagent.Dial(addr)
	if err != nil {
		t.Fatalf(" Dial 1: %v", err)
	}
	_, _ = c1.Write([]byte{0xDE, 0xAD})
	_ = c1.Close()

	// Give the server a moment to process the bad frame.
	time.Sleep(100 * time.Millisecond)

	// Connection 2: write a frame with an invalid message type, then EOF.
	c2, err := sshagent.Dial(addr)
	if err != nil {
		t.Fatalf("Dial 2: %v", err)
	}
	// length=5, type=0xFF (unknown), 4 garbage payload bytes
	_, _ = c2.Write([]byte{0x00, 0x00, 0x00, 0x05, 0xFF, 0x00, 0x00, 0x00, 0x00})
	_ = c2.Close()

	time.Sleep(100 * time.Millisecond)

	// Connection 3: a well-formed client must still succeed — server alive.
	c3, err := sshagent.Dial(addr)
	if err != nil {
		t.Fatalf("Dial 3 (post-malformed): %v", err)
	}
	defer c3.Close()
	cl := agent.NewClient(c3)
	keys, err := cl.List()
	if err != nil {
		t.Fatalf("client.List after malformed: %v (server died?)", err)
	}
	if len(keys) != 1 || keys[0].Comment != "malf-key" {
		t.Fatalf("post-malformed keys = %+v, want 1 malf-key", keys)
	}
}

// TestServerReturnsOnListenerClose: Serve must exit cleanly when the listener
// is closed, so the TUI can shut the agent down deterministically.
func TestServerReturnsOnListenerClose(t *testing.T) {
	addr := addrFor(t)
	ag := sshagent.NewKeyring()
	l, err := sshagent.Listen(addr)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- sshagent.Serve(l, ag) }()

	_ = l.Close()
	select {
	case err := <-done:
		if err == nil {
			// Serve may return nil or an error from Accept post-close; both ok.
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not return within 2s of listener close")
	}
}