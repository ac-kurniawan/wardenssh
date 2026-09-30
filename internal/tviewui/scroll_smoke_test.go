package tviewui_test

import (
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/ac-kurniawan/wardenssh/internal/hosts"
	"github.com/ac-kurniawan/wardenssh/internal/tviewui"
)

// scrollSmokeCmd prints a numbered banner then keeps running, so the session
// has real scrollback to move through and stays alive while the test drives it.
func scrollSmokeCmd() *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.Command("cmd", "/c", "for /L %i in (1,1,60) do @echo wardenssh-line-%i & ping -n 30 127.0.0.1 >nul")
	}
	return exec.Command("sh", "-c", "for i in $(seq 1 60); do echo wardenssh-line-$i; done; sleep 30")
}

// TestTerminalScrollSmokeRealSession drives a real shell over a real PTY and
// exercises the scroll paths end to end: the wheel at a plain shell moves the
// pane's own history and shows the marker, Shift+PgUp/End move history without
// reaching the shell, and typing goes to the shell and returns the view to the
// live output.
func TestTerminalScrollSmokeRealSession(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a real shell")
	}

	app := tviewui.New(hosts.NewList(nil), tviewui.Deps{}, nil)
	defer app.TerminalPane().Close()

	if err := app.TerminalPane().StartSSHFromCmd(
		hosts.Entry{Alias: "host-a", Source: "file", HostName: "127.0.0.1"},
		scrollSmokeCmd(), nil, func(error) {},
	); err != nil {
		t.Fatalf("start session: %v", err)
	}
	app.ShowTerminalPaneForTest()
	app.FocusTerminal()

	view := app.TerminalPane().ActiveViewForTest()
	if view == nil {
		t.Fatal("expected an active terminal view")
	}

	// Wait until the shell's own banner has reached the pane. Asserting on the
	// text (not just "some rows arrived") is what makes this a real end-to-end
	// check: the PTY output has to travel through the session into this view.
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(view.ScrollbackText(), "wardenssh-line-10") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	text := view.ScrollbackText()
	if !strings.Contains(text, "wardenssh-line-") {
		t.Fatalf("real session's banner never reached the pane; title=%q scrollback=%q",
			app.TerminalPane().ActiveTitle(), text)
	}
	if !strings.Contains(text, "wardenssh-line-10") {
		t.Fatalf("expected at least 10 lines of real output; got %q", text)
	}

	// The wheel at a plain shell scrolls the pane and shows the marker.
	view.ScrollbackUp(5)
	offset, _ := view.ScrollbackStatus()
	if offset == 0 {
		t.Fatal("wheel-up did not scroll the real session's history")
	}
	if title := app.TerminalPane().ActiveTitle(); !strings.Contains(title, "[↑ ") {
		t.Errorf("pane title after scrolling = %q, want a scroll marker", title)
	}

	// Shift+PgUp is the pane's key: history moves and the shell receives nothing.
	before := offset
	view.SendKey(tcell.NewEventKey(tcell.KeyPgUp, 0, tcell.ModShift))
	if after, _ := view.ScrollbackStatus(); after <= before {
		t.Errorf("Shift+PgUp did not scroll the pane: %d -> %d", before, after)
	}

	// Shift+End returns to the live output.
	view.SendKey(tcell.NewEventKey(tcell.KeyEnd, 0, tcell.ModShift))
	if offset, _ := view.ScrollbackStatus(); offset != 0 {
		t.Errorf("Shift+End did not return to the live output, offset=%d", offset)
	}
	if title := app.TerminalPane().ActiveTitle(); strings.Contains(title, "↑") {
		t.Errorf("pane title at the bottom = %q, want no marker", title)
	}

	// Typing reaches the shell and leaves the view at the bottom.
	view.SendKey(tcell.NewEventKey(tcell.KeyRune, 'e', tcell.ModNone))
	if offset, _ := view.ScrollbackStatus(); offset != 0 {
		t.Errorf("typing must leave the view at the live output, offset=%d", offset)
	}
}
