// Package askpass is Spike #3 (see .local/plan.md): it verifies the real
// platform OpenSSH client honors SSH_ASKPASS_REQUIRE=force when spawned as a
// child process under a real PTY — exactly how the TUI spawns ssh (ConPTY on
// Windows, pty on *nix). A console handle exists on stdin, which is the
// discriminating condition the risk is about: with a console attached, ssh
// would normally prompt on the tty, so only SSH_ASKPASS_REQUIRE=force makes it
// consult the askpass helper. The helper is the WardenSSH binary itself,
// re-exec'd in askpass mode (the production contract), so the spike also
// gates the WARDENSSH_ASKPASS=1 helper-mode flag. The proof is differential,
// both under a PTY: ssh + helper with the CORRECT password succeeds; with the
// WRONG password it fails with Permission denied; and with the correct
// password but the helper-mode flag MISSING it also fails — proving both the
// helper is genuinely consulted and the flag is load-bearing.
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

// buildWarden compiles the WardenSSH binary itself (module root package main)
// into a temp path — the SAME binary ssh spawns as the SSH_ASKPASS helper in
// production. Unlike a toy helper, this exercises the real askpass gate
// (main.runAskpass requires WARDENSSH_ASKPASS=1), so the spike covers the
// exact env contract EnvForAskpass sets — a bug where the flag was missing
// slipped past the earlier toy-helper version and caused "Permission denied".
func buildWarden(t *testing.T) string {
	t.Helper()
	exe := filepath.Join(t.TempDir(), "wardenssh")
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	if out, err := exec.Command("go", "build", "-o", exe, "../..").CombinedOutput(); err != nil {
		t.Fatalf("go build warden binary: %v\n%s", err, out)
	}
	return exe
}

// runSSH spawns ssh under a real PTY (go-pty, ConPTY on Windows) with the
// warden binary as the askpass helper, and returns (merged PTY output, exit
// error). This mirrors the TUI's spawn path (internal/session), so a console
// handle exists on ssh's stdin and SSH_ASKPASS_REQUIRE=force is what makes
// askpass work. askpassFlag toggles WARDENSSH_ASKPASS=1 — the helper-mode
// gate runAskpass requires; without it the helper launches the launcher TUI
// and ssh receives an empty password. A hung ssh is killed after spikeWait
// and reported as an error.
func runSSH(t *testing.T, pass string, askpassFlag bool) (string, error) {
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
	env := []string{
		"SSH_ASKPASS=" + buildWarden(t),
		"SSH_ASKPASS_REQUIRE=force",
		"WARDENSSH_ASKPASS_PASS=" + pass,
	}
	if askpassFlag {
		env = append(env, "WARDENSSH_ASKPASS=1")
	}
	c.Env = append(os.Environ(), env...)
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
	out, err := runSSH(t, goodPass, true)
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
	out, err := runSSH(t, badPass, true)
	if err == nil {
		t.Fatal("ssh with WRONG askpass password unexpectedly succeeded")
	}
	if !bytes.Contains([]byte(out), []byte("Permission denied")) {
		t.Fatalf("expected Permission denied, got: %q", out)
	}
}

// TestAskpassWithoutHelperFlagFails: regression guard — with the CORRECT
// password but WARDENSSH_ASKPASS unset, the re-exec'd helper launches the
// launcher TUI instead of printing the password, so ssh receives an empty
// password and auth fails. This is the exact bug that caused "Permission
// denied" for real users before EnvForAskpass set the flag.
func TestAskpassWithoutHelperFlagFails(t *testing.T) {
	if os.Getenv("WARDENSSH_ASKPASS_SPIKE") != "1" {
		t.Skip("askpass spike skipped (set WARDENSSH_ASKPASS_SPIKE=1 to run)")
	}
	out, err := runSSH(t, goodPass, false)
	if err == nil {
		t.Fatal("ssh without WARDENSSH_ASKPASS=1 unexpectedly succeeded")
	}
	if !bytes.Contains([]byte(out), []byte("Permission denied")) {
		t.Fatalf("expected Permission denied (empty helper output), got: %q", out)
	}
}
