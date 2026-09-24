//go:build !windows

package connect

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultAgentPipeIsPrivatePerProcess(t *testing.T) {
	a := defaultAgentPipe()
	b := defaultAgentPipe()
	if a != b {
		t.Fatalf("defaultAgentPipe changed within one process: %s vs %s", a, b)
	}
	if a == "/tmp/wardenssh-agent.sock" || strings.HasSuffix(a, "/wardenssh-agent.sock") && filepath.Dir(a) == "/tmp" {
		t.Fatalf("path %s is the shared /tmp socket", a)
	}
	wantBase := fmt.Sprintf("agent-%d.sock", os.Getpid())
	if filepath.Base(a) != wantBase {
		t.Fatalf("socket name = %s, want %s", filepath.Base(a), wantBase)
	}
	dir := filepath.Dir(a)
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("Stat %s: %v", dir, err)
	}
	if !info.IsDir() {
		t.Fatalf("%s is not a directory", dir)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("dir mode = %o, want 0700", got)
	}
}
