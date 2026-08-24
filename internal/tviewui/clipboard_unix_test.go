//go:build !windows

package tviewui

import (
	"bytes"
	"encoding/base64"
	"errors"
	"testing"
)

// forceNoNative disables native clipboard tool detection so copyToClipboard
// always falls back to the OSC 52 escape sequence. Restores state on cleanup.
func forceNoNative(t *testing.T) {
	t.Helper()
	origEnv := lookupEnv
	origPath := lookPath
	lookupEnv = func(string) string { return "" }
	lookPath = func(string) (string, error) { return "", errors.New("not found") }
	t.Cleanup(func() {
		lookupEnv = origEnv
		lookPath = origPath
	})
}

// TestCopyToClipboardEmitsOSC52: with no native clipboard tool available,
// copyToClipboard must write an OSC 52 "set clipboard" sequence (base64
// payload, BEL terminator) to the terminal.
func TestCopyToClipboardEmitsOSC52(t *testing.T) {
	forceNoNative(t)
	var buf bytes.Buffer
	origW := clipboardWriter
	clipboardWriter = &buf
	t.Cleanup(func() { clipboardWriter = origW })

	if err := copyToClipboard("hello world"); err != nil {
		t.Fatalf("copyToClipboard: %v", err)
	}
	want := "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte("hello world")) + "\x07"
	if buf.String() != want {
		t.Errorf("output = %q, want %q", buf.String(), want)
	}
}

// TestCopyToClipboardEmptyWritesNothing: an empty selection writes nothing and
// invokes no clipboard tool.
func TestCopyToClipboardEmptyWritesNothing(t *testing.T) {
	forceNoNative(t)
	var buf bytes.Buffer
	origW := clipboardWriter
	clipboardWriter = &buf
	t.Cleanup(func() { clipboardWriter = origW })

	if err := copyToClipboard(""); err != nil {
		t.Fatalf("copyToClipboard: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("expected no output for empty text, got %q", buf.String())
	}
}

// TestCopyToClipboardWaylandUsesWlCopy: on a Wayland session with wl-copy on
// PATH, copyToClipboard pipes the text to wl-copy and does not emit OSC 52.
func TestCopyToClipboardWaylandUsesWlCopy(t *testing.T) {
	var calls [][]string
	var stdins []string
	origRun := runCmd
	origEnv := lookupEnv
	origPath := lookPath
	runCmd = func(name string, args []string, stdin string) error {
		calls = append(calls, append([]string{name}, args...))
		stdins = append(stdins, stdin)
		return nil
	}
	lookupEnv = func(k string) string {
		if k == "WAYLAND_DISPLAY" {
			return "wayland-0"
		}
		return ""
	}
	lookPath = func(name string) (string, error) {
		if name == "wl-copy" {
			return "/usr/bin/wl-copy", nil
		}
		return "", errors.New("not found")
	}
	t.Cleanup(func() { runCmd, lookupEnv, lookPath = origRun, origEnv, origPath })
	var osc bytes.Buffer
	origW := clipboardWriter
	clipboardWriter = &osc
	t.Cleanup(func() { clipboardWriter = origW })

	if err := copyToClipboard("hello"); err != nil {
		t.Fatalf("copyToClipboard: %v", err)
	}
	if len(calls) != 1 || calls[0][0] != "wl-copy" {
		t.Fatalf("expected a single wl-copy invocation, got %v", calls)
	}
	if len(stdins) != 1 || stdins[0] != "hello" {
		t.Errorf("expected stdin %q, got %q", "hello", stdins)
	}
	if osc.Len() != 0 {
		t.Errorf("OSC 52 must not be emitted when wl-copy succeeds, got %q", osc.String())
	}
}

// TestCopyToClipboardX11UsesXclip: on X11 with xclip on PATH, copyToClipboard
// pipes the text to `xclip -selection clipboard`.
func TestCopyToClipboardX11UsesXclip(t *testing.T) {
	var calls [][]string
	var stdins []string
	origRun := runCmd
	origEnv := lookupEnv
	origPath := lookPath
	runCmd = func(name string, args []string, stdin string) error {
		calls = append(calls, append([]string{name}, args...))
		stdins = append(stdins, stdin)
		return nil
	}
	lookupEnv = func(k string) string {
		if k == "DISPLAY" {
			return ":0"
		}
		return ""
	}
	lookPath = func(name string) (string, error) {
		if name == "xclip" {
			return "/usr/bin/xclip", nil
		}
		return "", errors.New("not found")
	}
	t.Cleanup(func() { runCmd, lookupEnv, lookPath = origRun, origEnv, origPath })

	if err := copyToClipboard("hi"); err != nil {
		t.Fatalf("copyToClipboard: %v", err)
	}
	if len(calls) != 1 || calls[0][0] != "xclip" {
		t.Fatalf("expected a single xclip invocation, got %v", calls)
	}
	wantArgs := []string{"xclip", "-selection", "clipboard"}
	if calls[0][0] != wantArgs[0] {
		t.Errorf("tool = %q, want %q", calls[0][0], wantArgs[0])
	}
	if len(calls[0]) != 3 || calls[0][1] != "-selection" || calls[0][2] != "clipboard" {
		t.Errorf("args = %v, want %v", calls[0], wantArgs)
	}
	if len(stdins) != 1 || stdins[0] != "hi" {
		t.Errorf("expected stdin %q, got %q", "hi", stdins)
	}
}

// TestCopyToClipboardX11FallsBackToXsel: on X11 with no xclip but xsel on
// PATH, copyToClipboard uses `xsel --clipboard --input`.
func TestCopyToClipboardX11FallsBackToXsel(t *testing.T) {
	var calls [][]string
	origRun := runCmd
	origEnv := lookupEnv
	origPath := lookPath
	runCmd = func(name string, args []string, stdin string) error {
		calls = append(calls, append([]string{name}, args...))
		return nil
	}
	lookupEnv = func(k string) string {
		if k == "DISPLAY" {
			return ":0"
		}
		return ""
	}
	lookPath = func(name string) (string, error) {
		if name == "xsel" {
			return "/usr/bin/xsel", nil
		}
		return "", errors.New("not found")
	}
	t.Cleanup(func() { runCmd, lookupEnv, lookPath = origRun, origEnv, origPath })

	if err := copyToClipboard("hi"); err != nil {
		t.Fatalf("copyToClipboard: %v", err)
	}
	if len(calls) != 1 || calls[0][0] != "xsel" {
		t.Fatalf("expected a single xsel invocation, got %v", calls)
	}
	if len(calls[0]) != 3 || calls[0][1] != "--clipboard" || calls[0][2] != "--input" {
		t.Errorf("args = %v, want [xsel --clipboard --input]", calls[0])
	}
}

// TestCopyToClipboardFallsBackToOSC52OnToolFailure: if the native tool exists
// but fails to run, copyToClipboard falls back to emitting OSC 52.
func TestCopyToClipboardFallsBackToOSC52OnToolFailure(t *testing.T) {
	origRun := runCmd
	origEnv := lookupEnv
	origPath := lookPath
	runCmd = func(name string, args []string, stdin string) error {
		return errors.New("boom")
	}
	lookupEnv = func(k string) string {
		if k == "WAYLAND_DISPLAY" {
			return "wayland-0"
		}
		return ""
	}
	lookPath = func(name string) (string, error) {
		if name == "wl-copy" {
			return "/usr/bin/wl-copy", nil
		}
		return "", errors.New("not found")
	}
	t.Cleanup(func() { runCmd, lookupEnv, lookPath = origRun, origEnv, origPath })
	var osc bytes.Buffer
	origW := clipboardWriter
	clipboardWriter = &osc
	t.Cleanup(func() { clipboardWriter = origW })

	if err := copyToClipboard("x"); err != nil {
		t.Fatalf("copyToClipboard: %v", err)
	}
	want := "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte("x")) + "\x07"
	if osc.String() != want {
		t.Errorf("expected OSC 52 fallback %q, got %q", want, osc.String())
	}
}
