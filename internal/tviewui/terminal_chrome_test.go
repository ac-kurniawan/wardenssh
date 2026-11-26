package tviewui

import (
	"strings"
	"testing"
)

// TestRightColumnMountsSessionHeaderAndTerminal: the right column must be
// tab bar (1 row) + session header (1 row) + terminal (rest). The header used
// to be constructed but never mounted, so its fields were invisible while the
// terminal border title repeated them.
func TestRightColumnMountsSessionHeaderAndTerminal(t *testing.T) {
	app := New(copyTestHostList(), Deps{}, nil)
	defer app.TerminalPane().Close()
	// The header probes the host on a ticker; keep the test off the network.
	app.SessionHeader().SetDialFuncForTest(func(host, port string) (int, bool) { return 1, true })

	app.TerminalPane().SetSessionForTest(copyTestKey(), "host-a", "file")
	app.ShowTerminalPaneForTest()

	if got, want := app.right.GetItemCount(), 3; got != want {
		t.Fatalf("right column items = %d, want %d (tab bar, header, terminal)", got, want)
	}
	if got, want := app.right.GetItem(0), app.TabBar().Primitive(); got != want {
		t.Errorf("right[0] = %T, want the tab bar", got)
	}
	if got, want := app.right.GetItem(1), app.SessionHeader().Primitive(); got != want {
		t.Errorf("right[1] = %T, want the session header", got)
	}
	if got, want := app.right.GetItem(2), app.TerminalPane().Primitive(); got != want {
		t.Errorf("right[2] = %T, want the terminal pane", got)
	}
}

// TestTerminalTitleIsIdentityOnly: the border title owns the identity only.
// State, uptime, ping and agent live in the mounted session header — repeating
// them in the title is what collided with the tab bar on narrow panes.
func TestTerminalTitleIsIdentityOnly(t *testing.T) {
	app := New(copyTestHostList(), Deps{}, nil)
	defer app.TerminalPane().Close()
	app.SessionHeader().SetDialFuncForTest(func(host, port string) (int, bool) { return 1, true })

	// A vault-sourced session is the strictest case: it is the one that used to
	// carry the "• Agent: RAM-only" suffix in the border title.
	key := SessionKey("prod-db-01", "vw:work")
	app.TerminalPane().SetSessionForTest(key, "prod-db-01", "vw:work")
	view, _ := fedView(t)
	defer view.Close()
	app.TerminalPane().SetSessionViewForTest(key, view)
	// Activate re-renders the session chrome onto the freshly attached view.
	app.TerminalPane().Activate(key)

	title := view.TerminalTitle()
	if !strings.Contains(title, "prod-db-01") {
		t.Fatalf("terminal title = %q, want the alias", title)
	}
	for _, banned := range []string{"[", "Up:", "ms]", "Agent:"} {
		if strings.Contains(title, banned) {
			t.Errorf("terminal title = %q, must not repeat the header's %q", title, banned)
		}
	}

	// The state badge still reaches the user — via the header, not the border.
	app.FocusTerminal()
	if got := app.SessionHeader().Text(); !strings.Contains(got, "ACTIVE SESSION") {
		t.Errorf("session header = %q, want ACTIVE SESSION while the terminal has focus", got)
	}
	app.FocusHostList()
	if got := app.SessionHeader().Text(); !strings.Contains(got, "CONNECTED") {
		t.Errorf("session header = %q, want CONNECTED while the host list has focus", got)
	}
}
