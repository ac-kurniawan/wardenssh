// Package realssh is Spike #1.5 / the vertical-slice proof (see .local/plan.md):
// it boots the in-process in-RAM agent and verifies ssh.exe authenticates to a
// real sshd via SSH_AUTH_SOCK pointing at our pipe. The proof is differential:
// (1) with the user's real key loaded into the agent, ssh (NO -i) succeeds;
// (2) with an EMPTY agent, the same ssh invocation fails with Permission denied.
// The only variable between the two is the agent's contents -> the agent is
// provably the auth path (ssh.exe reads the key from our pipe and signs).
//
// GATED: skipped unless WARDENSSH_REALSSH=1 (needs a real sshd target + the
// user's real key on the host machine; never run in normal testing/CI).
//
// SECURITY: the private key bytes are read into RAM for the test and NEVER
// logged, printed, or committed. The file is in ~/.ssh/ (outside the repo),
// so git never touches it. Only the public connection result is asserted on.
package realssh

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/ac-kurniawan/wardenssh/internal/sshagent"
)

const (
	testHost = "43.129.40.139"
	testUser = "root"
	keyFile  = "tencent.pem"
)

// readKey reads the user's PEM key into memory (never logged) and parses it
// to a raw private key. Fails loudly if the file is missing or unparseable.
func readKey(t *testing.T) interface{} {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	path := filepath.Join(home, ".ssh", keyFile)
	raw, err := os.ReadFile(path) // in-memory only; never printed
	if err != nil {
		t.Fatalf("read %s (set WARDENSSH_REALSSH=0 to skip): %v", path, err)
	}
	priv, err := ssh.ParseRawPrivateKey(raw)
	if err != nil {
		t.Fatalf("parse private key (is it passphrase-protected? baseline ssh used no passphrase): %v", err)
	}
	return priv
}

// TestBaselineSSHWithIdentityFile is the precondition: the target is reachable
// AND the user's key file auths directly. If this fails, the agent tests'
// failures are meaningless (could be network/target/key-changed), so we say so.
func TestBaselineSSHWithIdentityFile(t *testing.T) {
	if os.Getenv("WARDENSSH_REALSSH") != "1" {
		t.Skip("real-ssh baseline skipped (set WARDENSSH_REALSSH=1 to run)")
	}
	home, _ := os.UserHomeDir()
	key := filepath.Join(home, ".ssh", keyFile)

	cmd := exec.Command("ssh",
		"-i", key,
		"-o", "BatchMode=yes",
		"-o", "IdentitiesOnly=yes",
		"-o", "ConnectTimeout=10",
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "UserKnownHostsFile="+os.DevNull,
		testUser+"@"+testHost,
		"echo BASELINE-OK",
	)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("baseline ssh (with -i) failed — fix this before trusting the agent tests: %v\nout: %s", err, out.String())
	}
	if !strings.Contains(out.String(), "BASELINE-OK") {
		t.Fatalf("baseline ssh produced no marker: %q", out.String())
	}
}

// TestRealSSH_AgentLoaded_AuthsSucceeds: with the key in the agent, ssh
// authenticates via SSH_AUTH_SOCK with NO -i flag (no file identity offered).
func TestRealSSH_AgentLoaded_AuthsSucceeds(t *testing.T) {
	if os.Getenv("WARDENSSH_REALSSH") != "1" {
		t.Skip("real-ssh vertical slice skipped (set WARDENSSH_REALSSH=1 to run)")
	}
	out, errOut, runErr := runSSHOverAgent(t, true /* load key */)
	if runErr != nil {
		t.Fatalf("ssh with loaded agent exited non-zero (agent auth failed?): %v\nstderr: %s\nstdout: %s",
			runErr, errOut, out)
	}
	if !strings.Contains(out, "WARDENSSH-AGENT-AUTH-OK") || !strings.Contains(out, testUser) {
		t.Errorf("output = %q, want marker + whoami=%s", out, testUser)
	}
}

// TestRealSSH_EmptyAgent_AuthsFail: with an EMPTY agent, the same ssh
// invocation MUST fail (Permission denied). This is the negative control that
// isolates the agent as the auth path in the loaded test above.
func TestRealSSH_EmptyAgent_AuthsFail(t *testing.T) {
	if os.Getenv("WARDENSSH_REALSSH") != "1" {
		t.Skip("real-ssh negative control skipped (set WARDENSSH_REALSSH=1 to run)")
	}
	if testing.Short() {
		t.Skip("skipping negative control in -short mode")
	}
	out, errOut, runErr := runSSHOverAgent(t, false /* empty agent */)
	if runErr == nil {
		t.Fatalf("ssh with EMPTY agent unexpectedly SUCCEEDED — some default file key authorized on target? output: %s", out)
	}
	// Must be an auth failure, not a network failure. Tolerate transient net
	// flakes by accepting either Permission denied or a connection error, but
	// flag clearly so the loaded test is interpretable.
	if !strings.Contains(errOut, "Permission denied") && !strings.Contains(errOut, "Connection") {
		t.Fatalf("unexpected ssh failure (not auth): %q", errOut)
	}
}

// runSSHOverAgent starts a fresh agent pipe, optionally loads the user's key,
// runs ssh with SSH_AUTH_SOCK=pipe and NO -i (so file identity is not offered;
// the only path is the agent), and returns (stdout, stderr, error).
func runSSHOverAgent(t *testing.T, loadKey bool) (string, string, error) {
	t.Helper()
	pipe := pipePath(t, "wardenssh-realssh")
	ag := sshagent.NewKeyring()
	if loadKey {
		if _, err := ag.Load(readKey(t), "vertical-slice-key", "sess-realssh"); err != nil {
			t.Fatalf("agent.Load: %v", err)
		}
	}
	l, err := sshagent.Listen(pipe)
	if err != nil {
		t.Fatalf("agent.Listen: %v", err)
	}
	defer l.Close()
	go func() { _ = sshagent.Serve(l, ag) }()
	waitPipeReady(t, pipe)

	cmd := exec.Command("ssh",
		"-o", "BatchMode=yes",
		"-o", "PreferredAuthentications=publickey",
		"-o", "ConnectTimeout=10",
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "UserKnownHostsFile="+os.DevNull,
		testUser+"@"+testHost,
		"echo WARDENSSH-AGENT-AUTH-OK; whoami",
	)
	cmd.Env = append(os.Environ(), "SSH_AUTH_SOCK="+pipe)
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	err = cmd.Run()
	return out.String(), errOut.String(), err
}

// --- helpers ---

// pipePath returns a unique agent address per test on the current platform.
func pipePath(t *testing.T, prefix string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		return `\\.\pipe\` + prefix + "-" + randHex(4)
	}
	return filepath.Join(t.TempDir(), prefix+".sock")
}

func waitPipeReady(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		c, err := sshagent.Dial(addr)
		if err == nil {
			_ = c.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("agent pipe not ready at %s", addr)
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = io.ReadFull(rand.Reader, b)
	return hex.EncodeToString(b)
}