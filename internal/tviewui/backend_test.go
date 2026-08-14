package tviewui_test

import (
	"bytes"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ac-kurniawan/wardenssh/internal/tviewui"
)

func TestPtyBackendReadsOutput(t *testing.T) {
	marker := "WARDENSSH-BACKEND-TEST"
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("powershell", "-Command", "Write-Output "+marker)
	} else {
		cmd = exec.Command("echo", marker)
	}

	backend, err := tviewui.NewPtyBackend(cmd, 80, 24)
	if err != nil {
		t.Fatalf("NewPtyBackend: %v", err)
	}
	defer backend.Close()

	var acc bytes.Buffer
	done := make(chan struct{})
	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := backend.Read(buf)
			if n > 0 {
				acc.Write(buf[:n])
				if strings.Contains(acc.String(), marker) {
					close(done)
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatalf("backend.Read timed out after 30s; accumulated output: %q", acc.String())
	}
}

func TestPtyBackendWriteSendsInput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("cat not available on Windows")
	}
	cmd := exec.Command("cat")
	backend, err := tviewui.NewPtyBackend(cmd, 80, 24)
	if err != nil {
		t.Fatalf("NewPtyBackend: %v", err)
	}
	defer backend.Close()

	input := []byte("hello-pty\n")
	if _, err := backend.Write(input); err != nil {
		t.Fatalf("Write: %v", err)
	}

	buf := make([]byte, 1024)
	done := make(chan struct{})
	var n int
	go func() {
		n, _ = backend.Read(buf)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("backend.Read timed out after 5s")
	}
	if !strings.Contains(string(buf[:n]), "hello-pty") {
		t.Errorf("read = %q, want it to contain 'hello-pty'", string(buf[:n]))
	}
}

func TestPtyBackendCloseTerminates(t *testing.T) {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/c", "ping -n 30 127.0.0.1")
	} else {
		cmd = exec.Command("sleep", "30")
	}
	backend, err := tviewui.NewPtyBackend(cmd, 80, 24)
	if err != nil {
		t.Fatalf("NewPtyBackend: %v", err)
	}
	if err := backend.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	_ = os.Stderr
}



