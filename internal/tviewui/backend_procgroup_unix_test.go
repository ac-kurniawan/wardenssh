//go:build !windows

package tviewui_test

import (
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"

	"github.com/ac-kurniawan/wardenssh/internal/session"
	"github.com/ac-kurniawan/wardenssh/internal/tviewui"
)

// TestPtyBackendCloseReapsGrandchild spawns a wrapper plus a HUP-ignoring
// child. Close must kill the process group, not only the wrapper, so an
// ssh -J or SSH_ASKPASS grandchild cannot survive the session.
func TestPtyBackendCloseReapsGrandchild(t *testing.T) {
	dir := t.TempDir()
	childPIDFile := dir + "/child.pid"
	script := dir + "/wrapper.sh"
	body := "#!/bin/sh\n" +
		"sh -c 'trap \"\" HUP; exec sleep 60' &\n" +
		"echo $! > " + strconv.Quote(childPIDFile) + "\n" +
		"exec sleep 60\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatalf("write wrapper: %v", err)
	}

	cmd := exec.Command(script)
	backend, err := tviewui.NewPtyBackend(cmd, 80, 24)
	if err != nil {
		t.Fatalf("NewPtyBackend: %v", err)
	}
	t.Cleanup(func() { _ = backend.Close() })

	var childPID int
	deadline := time.Now().Add(2 * time.Second)
	for {
		b, err := os.ReadFile(childPIDFile)
		if err == nil && len(b) > 0 {
			childPID, err = strconv.Atoi(string(trimSpace(b)))
			if err == nil && childPID > 1 {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("wrapper did not record grandchild pid")
		}
		time.Sleep(20 * time.Millisecond)
	}

	if err := backend.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	deadline = time.Now().Add(2 * time.Second)
	for session.ProcessAlive(childPID) {
		if time.Now().After(deadline) {
			t.Fatalf("pid %d still alive after Close", childPID)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func trimSpace(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r' || b[len(b)-1] == ' ') {
		b = b[:len(b)-1]
	}
	return b
}
