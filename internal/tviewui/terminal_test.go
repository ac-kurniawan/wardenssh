package tviewui_test

import (
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ac-kurniawan/wardenssh/internal/hosts"
	"github.com/ac-kurniawan/wardenssh/internal/tviewui"
)

func longRunningCmd() *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.Command("cmd", "/c", "ping -n 30 127.0.0.1 >nul")
	}
	return exec.Command("sleep", "30")
}

func shortCmd() *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.Command("cmd", "/c", "echo WARDENSSH-TERM-EXIT")
	}
	return exec.Command("echo", "WARDENSSH-TERM-EXIT")
}

func keyA() string { return tviewui.SessionKey("host-a", "file") }
func keyB() string { return tviewui.SessionKey("host-b", "file") }

func entryA() hosts.Entry { return hosts.Entry{Alias: "host-a", Source: "file"} }
func entryB() hosts.Entry { return hosts.Entry{Alias: "host-b", Source: "file"} }

// TestTerminalPaneMultipleSessions: the pane can host N concurrent sessions
// (Q18/iii yield-and-switch). Starting a second session keeps the first
// running; both report running; the most recently started is active.
func TestTerminalPaneMultipleSessions(t *testing.T) {
	pane := tviewui.NewTerminalPane(nil)
	defer pane.Close()

	if err := pane.StartSSHFromCmd(entryA(), longRunningCmd(), nil, func(error) {}); err != nil {
		t.Fatalf("start a: %v", err)
	}
	if err := pane.StartSSHFromCmd(entryB(), longRunningCmd(), nil, func(error) {}); err != nil {
		t.Fatalf("start b: %v", err)
	}

	if !pane.IsRunning() {
		t.Fatal("expected pane to be running with 2 sessions")
	}
	if n := pane.SessionCount(); n != 2 {
		t.Errorf("SessionCount = %d, want 2", n)
	}
	if alias, source, ok := pane.ActiveEntry(); !ok || alias != "host-b" || source != "file" {
		t.Errorf("ActiveEntry = %q/%q/%v, want host-b/file/true", alias, source, ok)
	}
	if !pane.HasSession(keyA()) || !pane.HasSession(keyB()) {
		t.Error("expected both sessions to be tracked")
	}
}

// TestTerminalPaneCloseSessionKeepsOthers: closing one session does not affect
// the other; the remaining session becomes active.
func TestTerminalPaneCloseSessionKeepsOthers(t *testing.T) {
	pane := tviewui.NewTerminalPane(nil)
	defer pane.Close()

	if err := pane.StartSSHFromCmd(entryA(), longRunningCmd(), nil, func(error) {}); err != nil {
		t.Fatalf("start a: %v", err)
	}
	if err := pane.StartSSHFromCmd(entryB(), longRunningCmd(), nil, func(error) {}); err != nil {
		t.Fatalf("start b: %v", err)
	}

	pane.CloseSession(keyA())

	if pane.HasSession(keyA()) {
		t.Error("expected host-a session to be removed after CloseSession")
	}
	if !pane.HasSession(keyB()) {
		t.Error("expected host-b session to survive CloseSession(host-a)")
	}
	if alias, _, ok := pane.ActiveEntry(); !ok || alias != "host-b" {
		t.Errorf("ActiveEntry = %q/%v after closing host-a, want host-b/true", alias, ok)
	}
}

// TestTerminalPaneExitRemovesOnlyItsSession: when one session's process exits,
// only that session is removed; the other keeps running.
func TestTerminalPaneExitRemovesOnlyItsSession(t *testing.T) {
	pane := tviewui.NewTerminalPane(nil)
	defer pane.Close()

	exited := make(chan string, 1)
	if err := pane.StartSSHFromCmd(entryA(), shortCmd(), nil, func(error) {
		exited <- keyA()
	}); err != nil {
		t.Fatalf("start a: %v", err)
	}
	if err := pane.StartSSHFromCmd(entryB(), longRunningCmd(), nil, func(error) {}); err != nil {
		t.Fatalf("start b: %v", err)
	}

	select {
	case who := <-exited:
		if who != keyA() {
			t.Fatalf("exit callback for %q, want host-a", who)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("host-a did not exit within 5s")
	}

	if pane.HasSession(keyA()) {
		t.Error("expected exited session host-a to be removed")
	}
	if !pane.HasSession(keyB()) {
		t.Error("expected host-b to still be running")
	}
	if n := pane.SessionCount(); n != 1 {
		t.Errorf("SessionCount = %d, want 1", n)
	}
}

// TestTerminalPaneExitOfActiveSessionSwitchesToOther: when the ACTIVE session
// exits, the pane activates the remaining session.
func TestTerminalPaneExitOfActiveSessionSwitchesToOther(t *testing.T) {
	pane := tviewui.NewTerminalPane(nil)
	defer pane.Close()

	if err := pane.StartSSHFromCmd(entryA(), longRunningCmd(), nil, func(error) {}); err != nil {
		t.Fatalf("start a: %v", err)
	}
	exited := make(chan struct{})
	if err := pane.StartSSHFromCmd(entryB(), shortCmd(), nil, func(error) {
		close(exited)
	}); err != nil {
		t.Fatalf("start b: %v", err)
	}

	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		t.Fatal("host-b (active) did not exit within 5s")
	}

	// host-a should now be active.
	if alias, _, ok := pane.ActiveEntry(); !ok || alias != "host-a" {
		t.Errorf("ActiveEntry = %q/%v after host-b exit, want host-a/true", alias, ok)
	}
	if !pane.IsRunning() {
		t.Error("pane should still be running with host-a")
	}
}

// TestTerminalPaneStartsAndExits: single session lifecycle (regression).
func TestTerminalPaneStartsAndExits(t *testing.T) {
	pane := tviewui.NewTerminalPane(nil)

	exited := make(chan error, 1)
	err := pane.StartSSHFromCmd(hosts.Entry{Alias: "test", Source: "file"}, shortCmd(), nil, func(e error) {
		exited <- e
	})
	if err != nil {
		t.Fatalf("StartSSHFromCmd: %v", err)
	}
	defer pane.Close()

	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		t.Fatal("terminal pane did not signal exit within 5s")
	}

	if pane.IsRunning() {
		t.Error("expected IsRunning() == false after exit")
	}
}

// TestTerminalPaneNotRunningInitially (regression).
func TestTerminalPaneNotRunningInitially(t *testing.T) {
	pane := tviewui.NewTerminalPane(nil)
	if pane.IsRunning() {
		t.Error("expected IsRunning() == false before StartSSH")
	}
}

func TestFormatSessionTitle(t *testing.T) {
	if got := tviewui.FormatSessionTitleForTest("tencent1", "43.129.40.8", "CONNECTED", 0); got != "💻 tencent1 (43.129.40.8) [CONNECTED] · Up: 0s" {
		t.Errorf("title = %q", got)
	}
	if got := tviewui.FormatSessionTitleForTest("tencent1", "43.129.40.8", "ACTIVE SESSION", 90*time.Second); !strings.Contains(got, "[ACTIVE SESSION]") || !strings.Contains(got, "1m30s") {
		t.Errorf("title = %q", got)
	}
}

func TestTerminalPaneTitleState(t *testing.T) {
	pane := tviewui.NewTerminalPane(nil)
	defer pane.Close()
	key := tviewui.SessionKey("host-a", "file")
	pane.SetSessionForTest(key, "host-a", "file")
	pane.SetSessionTitleState(true)
	if !strings.Contains(pane.ActiveTitle(), "ACTIVE SESSION") {
		t.Errorf("focused title = %q, want ACTIVE SESSION", pane.ActiveTitle())
	}
	pane.SetSessionTitleState(false)
	if !strings.Contains(pane.ActiveTitle(), "CONNECTED") {
		t.Errorf("unfocused title = %q, want CONNECTED", pane.ActiveTitle())
	}
}
