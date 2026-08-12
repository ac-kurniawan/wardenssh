//go:build windows

package sshauthsock_test

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/ac-kurniawan/wardenssh/spike/sshauthsock"
)

// TestSSHAddListsKeyFromNamedPipeAgent is Spike #1 (.local/plan.md): the
// project-killing risk for Windows support. It verifies that Windows
// OpenSSH 9.5p2 honors SSH_AUTH_SOCK pointing at a Go-served named pipe.
//
// A named-pipe ssh-agent is started in-process, an ed25519 key with a
// distinctive comment is loaded, `ssh-add -l` is run with SSH_AUTH_SOCK set
// to the pipe path, and we assert ssh-add lists our key — proving ssh.exe
// honored SSH_AUTH_SOCK and spoke the agent protocol to our pipe (rather
// than silently falling back to the OS OpenSSH Agent service pipe).
func TestSSHAddListsKeyFromNamedPipeAgent(t *testing.T) {
	if os.Getenv("WARDENSSH_SKIP_SPIKE1") == "1" {
		t.Skip("spike #1 explicitly skipped")
	}
	sshAdd := findSSHAdd(t)
	pipePath := `\\.\pipe\wardenssh-spike-test`

	srv, err := sshauthsock.Start(pipePath)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Close()

	const comment = "wardenssh-spike-ed25519"
	if err := srv.AddEd25519(comment); err != nil {
		t.Fatalf("AddEd25519: %v", err)
	}

	// ssh-add may race the pipe's readiness; retry briefly.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		out, errOut, runErr := runSSHAdd(sshAdd, pipePath)
		if strings.Contains(out, comment) {
			t.Logf("PASS: ssh-add -l listed our key via SSH_AUTH_SOCK named pipe")
			t.Logf("output: %s", out)
			return
		}
		if runErr != nil {
			t.Logf("ssh-add -l err=%v stdout=%q stderr=%q (retrying)", runErr, out, errOut)
		}
		time.Sleep(200 * time.Millisecond)
	}

	t.Fatalf("ssh-add -l never listed our key (comment=%q) via SSH_AUTH_SOCK=%q. "+
		"Likely OpenSSH ignored SSH_AUTH_SOCK and used the default agent service pipe.",
		comment, pipePath)
}

// TestSSHAddWithNoIdentitiesIsClean is a companion signal: if our pipe is
// contacted but holds no keys, ssh-add -l should exit non-zero with a
// "no identities" message, NOT "could not connect". This distinguishes
// "pipe reached but empty" from "pipe ignored".
func TestSSHAddWithNoIdentitiesIsClean(t *testing.T) {
	if os.Getenv("WARDENSSH_SKIP_SPIKE1") == "1" {
		t.Skip("spike #1 explicitly skipped")
	}
	sshAdd := findSSHAdd(t)
	pipePath := `\\.\pipe\wardenssh-spike-empty`

	srv, err := sshauthsock.Start(pipePath)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Close()

	deadline := time.Now().Add(3 * time.Second)
	var lastOut, lastErr string
	var lastErr2 error
	for time.Now().Before(deadline) {
		lastOut, lastErr, lastErr2 = runSSHAdd(sshAdd, pipePath)
		// "no identities" means the pipe was reached but holds no keys.
		if strings.Contains(lastErr, "no identities") || strings.Contains(lastOut, "no identities") {
			t.Logf("PASS: empty pipe reached; ssh-add reported no identities")
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("ssh-add -l did not reach empty pipe as expected. stdout=%q stderr=%q err=%v",
		lastOut, lastErr, lastErr2)
}

func runSSHAdd(sshAdd, sshAuthSock string) (string, string, error) {
	cmd := exec.Command(sshAdd, "-l")
	cmd.Env = append(os.Environ(), "SSH_AUTH_SOCK="+sshAuthSock)
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	err := cmd.Run()
	return out.String(), errOut.String(), err
}

func findSSHAdd(t *testing.T) string {
	for _, p := range []string{
		`C:\WINDOWS\System32\OpenSSH\ssh-add.exe`,
		`C:\Windows\System32\OpenSSH\ssh-add.exe`,
	} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	p, err := exec.LookPath("ssh-add.exe")
	if err != nil {
		t.Fatalf("ssh-add.exe not found: %v", err)
	}
	return p
}