//go:build !windows

package sshagent_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ac-kurniawan/wardenssh/internal/sshagent"
)

func TestListenUnixSocketIsPrivate(t *testing.T) {
	dir := unixSocketDir(t)
	addr := filepath.Join(dir, "agent.sock")

	ln, err := sshagent.Listen(addr)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	info, err := os.Stat(addr)
	if err != nil {
		t.Fatalf("Stat socket: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("socket mode = %o, want 0600", got)
	}

	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("Stat dir: %v", err)
	}
	if got := dirInfo.Mode().Perm(); got&0o077 != 0 {
		t.Fatalf("parent dir mode = %o, want no group/other bits", got)
	}
}

func TestListenDoesNotUnlinkForeignSocket(t *testing.T) {
	dir := unixSocketDir(t)
	addr := filepath.Join(dir, "agent.sock")

	first, err := sshagent.Listen(addr)
	if err != nil {
		t.Fatalf("first Listen: %v", err)
	}
	t.Cleanup(func() { _ = first.Close() })

	conn, err := sshagent.Dial(addr)
	if err != nil {
		t.Fatalf("Dial before second Listen: %v", err)
	}
	_ = conn.Close()

	second, err := sshagent.Listen(addr)
	if err == nil {
		_ = second.Close()
		t.Fatal("second Listen succeeded; a live socket must not be unlinked")
	}

	conn, err = sshagent.Dial(addr)
	if err != nil {
		t.Fatalf("first listener was clobbered: Dial: %v", err)
	}
	_ = conn.Close()
}

func TestTwoListenersDoNotClobber(t *testing.T) {
	a := filepath.Join(unixSocketDir(t), "agent.sock")
	b := filepath.Join(unixSocketDir(t), "agent.sock")
	if a == b {
		t.Fatal("two listeners were given the same socket path")
	}

	la, err := sshagent.Listen(a)
	if err != nil {
		t.Fatalf("Listen a: %v", err)
	}
	t.Cleanup(func() { _ = la.Close() })

	lb, err := sshagent.Listen(b)
	if err != nil {
		t.Fatalf("Listen b: %v", err)
	}
	t.Cleanup(func() { _ = lb.Close() })

	ca, err := sshagent.Dial(a)
	if err != nil {
		t.Fatalf("Dial a: %v", err)
	}
	_ = ca.Close()

	cb, err := sshagent.Dial(b)
	if err != nil {
		t.Fatalf("Dial b: %v", err)
	}
	_ = cb.Close()
}
