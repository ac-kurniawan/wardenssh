// Package askpass is Spike #3 (see .local/plan.md): it verifies the real
// platform OpenSSH client honors SSH_ASKPASS_REQUIRE=force when spawned as a
// child process (the TUI spawns ssh via go-pty / ConPTY). The proof is
// differential: ssh + askpass helper with the CORRECT password succeeds;
// with the WRONG password it fails with Permission denied.
//
// GATED: skipped unless WARDENSSH_ASKPASS_SPIKE=1 (needs a provisioned
// password-auth sshd target; never run in normal testing/CI).
//
// SECURITY: the test password is a throwaway value for the throwaway sshd
// account only; it is never a real vault credential.
package askpass

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

const (
	testHost = "localhost"
	testPort = "2222"
	testUser = "test"
	goodPass = "testpass"
	badPass  = "wrongpass"
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

func runSSH(t *testing.T, pass string) (string, string, error) {
	t.Helper()
	cmd := exec.Command("ssh",
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "UserKnownHostsFile="+os.DevNull,
		"-o", "ConnectTimeout=10",
		"-p", testPort,
		testUser+"@"+testHost,
		"echo ASKPASS-OK",
	)
	cmd.Env = append(os.Environ(),
		"SSH_ASKPASS="+buildAskpass(t),
		"SSH_ASKPASS_REQUIRE=force",
		"WARDENSSH_ASKPASS_PASS="+pass,
	)
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	err := cmd.Run()
	return out.String(), errOut.String(), err
}

func TestAskpassCorrectPasswordAuthenticates(t *testing.T) {
	if os.Getenv("WARDENSSH_ASKPASS_SPIKE") != "1" {
		t.Skip("askpass spike skipped (set WARDENSSH_ASKPASS_SPIKE=1 to run)")
	}
	out, errOut, err := runSSH(t, goodPass)
	if err != nil {
		t.Fatalf("ssh with correct askpass password failed: %v\nstderr: %s", err, errOut)
	}
	if !bytes.Contains([]byte(out), []byte("ASKPASS-OK")) {
		t.Fatalf("no marker in output: %q", out)
	}
}

func TestAskpassWrongPasswordFails(t *testing.T) {
	if os.Getenv("WARDENSSH_ASKPASS_SPIKE") != "1" {
		t.Skip("askpass spike skipped (set WARDENSSH_ASKPASS_SPIKE=1 to run)")
	}
	_, errOut, err := runSSH(t, badPass)
	if err == nil {
		t.Fatal("ssh with WRONG askpass password unexpectedly succeeded")
	}
	if !bytes.Contains([]byte(errOut), []byte("Permission denied")) {
		t.Fatalf("expected Permission denied, got: %q", errOut)
	}
}