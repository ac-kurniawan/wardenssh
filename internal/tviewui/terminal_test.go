package tviewui_test

import (
	"os/exec"
	"runtime"
	"testing"
	"time"

	"github.com/ac-kurniawan/wardenssh/internal/hosts"
	"github.com/ac-kurniawan/wardenssh/internal/tviewui"
)

func TestTerminalPaneStartsAndExits(t *testing.T) {
	pane := tviewui.NewTerminalPane(nil) // app can be nil for test

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/c", "echo WARDENSSH-TERM-EXIT")
	} else {
		cmd = exec.Command("echo", "WARDENSSH-TERM-EXIT")
	}

	exited := make(chan error, 1)
	err := pane.StartSSHFromCmd(hosts.Entry{Alias: "test", Source: "file"}, cmd, nil, func(e error) {
		exited <- e
	})
	if err != nil {
		t.Fatalf("StartSSHFromCmd: %v", err)
	}
	defer pane.Close()

	select {
	case <-exited:
		// good — process exited
	case <-time.After(5 * time.Second):
		t.Fatal("terminal pane did not signal exit within 5s")
	}

	if pane.IsRunning() {
		t.Error("expected IsRunning() == false after exit")
	}
}

func TestTerminalPaneNotRunningInitially(t *testing.T) {
	pane := tviewui.NewTerminalPane(nil)
	if pane.IsRunning() {
		t.Error("expected IsRunning() == false before StartSSH")
	}
}
