// Package askpass is Spike #3 (see .local/plan.md): it verifies the real
// platform OpenSSH client honors SSH_ASKPASS_REQUIRE=force when spawned as a
// child process under a real PTY — exactly how the TUI spawns ssh (ConPTY on
// Windows, pty on *nix). A console handle exists on stdin, which is the
// discriminating condition the risk is about: with a console attached, ssh
// would normally prompt on the tty, so only SSH_ASKPASS_REQUIRE=force makes it
// consult the askpass helper. The proof is differential, both under a PTY:
// ssh + askpass helper with the CORRECT password succeeds; with the WRONG
// password it fails with Permission denied. Together they prove askpass is
// genuinely consulted (wrong pass fails) AND force is honored over a console
// (correct pass succeeds).
//
// GATED: skipped unless WARDENSSH_ASKPASS_SPIKE=1 (needs a provisioned
// password-auth sshd target; never run in normal testing/CI).
//
// SECURITY: the test password is a throwaway value for the throwaway sshd
// account only; it is never a real vault credential.
package askpass

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	pty "github.com/aymanbagabas/go-pty"
)

const (
	testHost   = "localhost"
	testPort   = "2222"
	testUser   = "test"
	goodPass   = "testpass"
	badPass    = "wrongpass"
	spikeWait  = 30 * time.Second // hard timeout: a hung ssh must fail the test, not hang it
	drainGrace = 250 * time.Millisecond
)

// buildAskpass compiles a tiny askpass helper into a platform executable (the
// same contract WardenSSH's runAskpass implements: print the password from
// WARDENSSH_ASKPASS_PASS). A shell script is NOT viable here — the spawned
// ssh.exe is a native Windows process and its spawn path has no sh.exe; the
// real design (Task 7) also re-execs the warden binary itself as the helper,
// so an .exe is the faithful stand-in.
func buildAskpass(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	code := `package main
import (
	"fmt"
	"os"
)
func main() {
	fmt.Print(os.Getenv("WARDENSSH_ASKPASS_PASS"))
}`
	if err := os.WriteFile(src, []byte(code), 0o600); err != nil {
		t.Fatalf("write askpass helper source: %v", err)
	}
	exe := filepath.Join(dir, "askpass")
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	if out, err := exec.Command("go", "build", "-o", exe, src).CombinedOutput(); err != nil {
		t.Fatalf("go build askpass helper: %v\n%s", err, out)
	}
	return exe
}

// runSSH spawns ssh under a real PTY (go-pty, ConPTY on Windows) with the
// askpass env, and returns (merged PTY output, exit error). This mirrors the
// TUI's spawn path (internal/session), so a console handle exists on ssh's
// stdin and SSH_ASKPASS_REQUIRE=force is what makes askpass work. A hung ssh
// is killed after spikeWait and reported as an error, so a force-broken build
// (ssh blocking on a console password prompt) fails the test instead of
// hanging it.
func runSSH(t *testing.T, pass string) (string, error) {
	t.Helper()
	p, err := pty.New()
	if err != nil {
		t.Fatalf("pty.New: %v", err)
	}
	_ = p.Resize(80, 24)

	c := p.Command("ssh",
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "UserKnownHostsFile="+os.DevNull,
		"-o", "ConnectTimeout=10",
		"-p", testPort,
		testUser+"@"+testHost,
		"echo ASKPASS-OK",
	)
	c.Env = append(os.Environ(),
		"SSH_ASKPASS="+buildAskpass(t),
		"SSH_ASKPASS_REQUIRE=force",
		"WARDENSSH_ASKPASS_PASS="+pass,
	)
	if err := c.Start(); err != nil {
		_ = p.Close()
		t.Fatalf("start ssh under PTY: %v", err)
	}

	// Drain the PTY output continuously (stdout+stderr are merged on a
	// console) until the PTY is closed below.
	var (
		mu     sync.Mutex
		out    bytes.Buffer
		reader = make(chan struct{})
	)
	go func() {
		defer close(reader)
		buf := make([]byte, 4096)
		for {
			n, err := p.Read(buf)
			if n > 0 {
				mu.Lock()
				out.Write(buf[:n])
				mu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()

	// Wait for ssh to exit (exactly once), with a hard timeout.
	wait := make(chan error, 1)
	go func() { wait <- c.Wait() }()

	var waitErr error
	timedOut := false
	select {
	case waitErr = <-wait:
	case <-time.After(spikeWait):
		timedOut = true
		if c.Process != nil {
			_ = c.Process.Kill()
		}
		select {
		case waitErr = <-wait:
		case <-time.After(5 * time.Second):
			waitErr = fmt.Errorf("process did not exit after kill")
		}
	}

	// ConPTY leaves the master pipe open after exit; give the reader a grace
	// period to drain remaining bytes, then close the PTY to unblock it.
	select {
	case <-reader:
	case <-time.After(drainGrace):
	}
	_ = p.Close()
	<-reader

	mu.Lock()
	output := out.String()
	mu.Unlock()

	if timedOut {
		return output, fmt.Errorf("ssh hung (killed after %s): %v", spikeWait, waitErr)
	}
	return output, waitErr
}

func TestAskpassCorrectPasswordAuthenticates(t *testing.T) {
	if os.Getenv("WARDENSSH_ASKPASS_SPIKE") != "1" {
		t.Skip("askpass spike skipped (set WARDENSSH_ASKPASS_SPIKE=1 to run)")
	}
	out, err := runSSH(t, goodPass)
	if err != nil {
		t.Fatalf("ssh with correct askpass password failed: %v\noutput: %s", err, out)
	}
	if !bytes.Contains([]byte(out), []byte("ASKPASS-OK")) {
		t.Fatalf("no marker in output: %q", out)
	}
}

func TestAskpassWrongPasswordFails(t *testing.T) {
	if os.Getenv("WARDENSSH_ASKPASS_SPIKE") != "1" {
		t.Skip("askpass spike skipped (set WARDENSSH_ASKPASS_SPIKE=1 to run)")
	}
	out, err := runSSH(t, badPass)
	if err == nil {
		t.Fatal("ssh with WRONG askpass password unexpectedly succeeded")
	}
	if !bytes.Contains([]byte(out), []byte("Permission denied")) {
		t.Fatalf("expected Permission denied, got: %q", out)
	}
}