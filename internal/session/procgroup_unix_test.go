//go:build !windows

package session_test

import (
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/ac-kurniawan/wardenssh/internal/session"
)

// TestKillReapsGrandchild starts a wrapper that forks a long-lived child and
// then sleeps. Killing the session must reap both, the way ssh -J and
// SSH_ASKPASS children must die with the session.
func TestKillReapsGrandchild(t *testing.T) {
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

	s, err := session.Start("pg", "host", "file", []string{script})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = s.Kill() })

	var childPID int
	deadline := time.Now().Add(2 * time.Second)
	for {
		b, err := os.ReadFile(childPIDFile)
		if err == nil && len(b) > 0 {
			childPID, err = strconv.Atoi(string(trimNL(b)))
			if err == nil && childPID > 1 {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("wrapper did not record grandchild pid")
		}
		time.Sleep(20 * time.Millisecond)
	}

	if err := s.Kill(); err != nil {
		t.Fatalf("Kill: %v", err)
	}

	waitDead := func(pid int) {
		t.Helper()
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if !session.ProcessAlive(pid) {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("pid %d still alive after Kill", pid)
	}
	waitDead(childPID)

	select {
	case <-s.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("session did not exit after Kill")
	}
}

func trimNL(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return b
}
