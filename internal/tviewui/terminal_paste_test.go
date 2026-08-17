package tviewui

import (
	"testing"

	"github.com/rivo/tview"
)

// TestTerminalPasteWritesSingleBracketedBlock: when a remote app has enabled
// bracketed paste (\x1b[?2004h), pasting multi-line text (e.g. formatted JSON)
// into the embedded terminal must be written to the PTY as ONE block wrapped
// in \x1b[200~ ... \x1b[201~. This is what lets vim/bash readline disable
// autoindent for the paste — otherwise each pasted newline re-triggers
// autoindent and the indentation compounds into the mess the user reported.
func TestTerminalPasteWritesSingleBracketedBlock(t *testing.T) {
	view := newTerminalView(nil, "host-a")
	view.SetRect(0, 0, 12, 5)
	defer view.Close()

	// Remote app enables bracketed paste, then emits enough output that the
	// mode switch is definitely processed by the emulator.
	payload := []byte("\x1b[?2004h")
	for i := 0; i < 30; i++ {
		payload = append(payload, []byte("xxxxxxxxxx\n")...)
	}
	backend := newTestBackend(payload)
	view.Attach(backend)
	waitScrollback(t, view)

	json := "{\n  \"id\": 1,\n  \"name\": \"Jane Doe\",\n  \"email\": \"jane.doe@example.com\"\n}"
	handler := view.PasteHandler()
	handler(json, func(p tview.Primitive) {})

	writes := backend.Writes()
	if len(writes) != 1 {
		t.Fatalf("expected exactly one write for the paste, got %d: %q", len(writes), writes)
	}
	want := "\x1b[200~" + json + "\x1b[201~"
	if string(writes[0]) != want {
		t.Errorf("paste write = %q, want bracketed block %q", string(writes[0]), want)
	}
}

// TestTerminalPasteRawWhenRemoteDisabled: if the remote app has NOT enabled
// bracketed paste, the pasted text is still delivered as a single raw write
// (never re-broken into per-line writes that autoindent would mangle).
func TestTerminalPasteRawWhenRemoteDisabled(t *testing.T) {
	view := newTerminalView(nil, "host-a")
	view.SetRect(0, 0, 12, 5)
	defer view.Close()

	payload := []byte("plain shell output\n")
	for i := 0; i < 30; i++ {
		payload = append(payload, []byte("xxxxxxxxxx\n")...)
	}
	backend := newTestBackend(payload)
	view.Attach(backend)
	waitScrollback(t, view)

	text := "a\n  b\n    c\n"
	handler := view.PasteHandler()
	handler(text, func(p tview.Primitive) {})

	writes := backend.Writes()
	if len(writes) != 1 {
		t.Fatalf("expected exactly one write for the paste, got %d: %q", len(writes), writes)
	}
	if string(writes[0]) != text {
		t.Errorf("paste write = %q, want raw text %q", string(writes[0]), text)
	}
}
