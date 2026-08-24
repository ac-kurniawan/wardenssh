//go:build !windows

package tviewui

import (
	"encoding/base64"
	"io"
	"os"
	"os/exec"
	"strings"
)

var (
	// clipboardWriter receives OSC 52 clipboard sequences on the fallback path.
	// Defaults to the process's stdout (the terminal). A package variable so
	// tests can capture output.
	clipboardWriter io.Writer = os.Stdout

	// runCmd runs an external clipboard tool with text supplied on stdin. A
	// package variable so tests can record invocations.
	runCmd = func(name string, args []string, stdin string) error {
		cmd := exec.Command(name, args...)
		cmd.Stdin = strings.NewReader(stdin)
		return cmd.Run()
	}

	// lookPath finds an executable on PATH. A package variable so tests can
	// control which tools are discoverable.
	lookPath = exec.LookPath

	// lookupEnv reads an environment variable. A package variable so tests can
	// control the detected display server.
	lookupEnv = os.Getenv
)

// copyToClipboard copies text to the OS clipboard. On unix it prefers the
// native Wayland/X11 clipboard tool (wl-copy / xclip / xsel) so it works even
// in terminals that do not support OSC 52 (e.g. GNOME Terminal / VTE). When no
// tool is available, or the tool fails, it falls back to the OSC 52 escape
// sequence, which the terminal emulator may or may not honor. This keeps the
// binary pure-Go with no hard external dependency: it still runs standalone,
// copy just degrades to OSC 52 (or nothing) when no tool is present.
func copyToClipboard(text string) error {
	if text == "" {
		return nil
	}
	if tool, args := nativeClipboardCommand(); tool != "" {
		if runCmd(tool, args, text) == nil {
			return nil
		}
	}
	seq := "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(text)) + "\x07"
	_, err := io.WriteString(clipboardWriter, seq)
	return err
}

// nativeClipboardCommand returns the clipboard tool and args for the current
// display server (Wayland → wl-copy; X11 → xclip, else xsel), or an empty tool
// name when none is available.
func nativeClipboardCommand() (string, []string) {
	if lookupEnv("WAYLAND_DISPLAY") != "" {
		if _, err := lookPath("wl-copy"); err == nil {
			return "wl-copy", nil
		}
	}
	if lookupEnv("DISPLAY") != "" {
		if _, err := lookPath("xclip"); err == nil {
			return "xclip", []string{"-selection", "clipboard"}
		}
		if _, err := lookPath("xsel"); err == nil {
			return "xsel", []string{"--clipboard", "--input"}
		}
	}
	return "", nil
}
