package tviewui

import (
	"fmt"
	"time"

	"github.com/mattn/go-runewidth"
	"github.com/rivo/tview"
)

// SessionHeader is the bar directly below the tab strip and above the
// terminal viewport — mirrors index-tui.html's session header:
// "● shopee-2 (20.198.88.91) [ACTIVE SESSION] Up: 12s • 42ms  [Copy][Disconnect][Force Kill]"
type SessionHeader struct {
	view *tview.TextView
}

// NewSessionHeader builds the header bar (h=1, no border).
func NewSessionHeader() *SessionHeader {
	v := tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignLeft)
	v.SetBorder(false)
	h := &SessionHeader{view: v}
	h.Clear()
	return h
}

// Primitive returns the tview primitive for layout embedding (height 1).
func (h *SessionHeader) Primitive() tview.Primitive { return h.view }

// Text returns the rendered text (tests).
func (h *SessionHeader) Text() string { return h.view.GetText(true) }

// RawText returns text with tags intact (tests).
func (h *SessionHeader) RawText() string { return h.view.GetText(false) }

// Clear shows the idle state when no session exists.
func (h *SessionHeader) Clear() {
	h.view.SetText("[#64748B]○ No active session — select a host from the left[-]")
}

// SetSession updates the header for the active session.
// alias/host/source from terminalSession, up is elapsed, focused controls badge.
func (h *SessionHeader) SetSession(alias, host, source string, up time.Duration, focused bool) {
	if alias == "" {
		h.Clear()
		return
	}
	addr := host
	if runewidth.StringWidth(addr) > 24 {
		addr = runewidth.Truncate(addr, 23, "…")
	}
	state := "CONNECTED"
	if focused {
		state = "ACTIVE SESSION"
	}
	upStr := formatUptime(up)

	// Left: dot + alias + (host) + state badge + uptime.
	dot := "[#64748B]○[-]"
	if source != "file" || true { // live dot always green when session exists
		dot = "[#22C55E]●[-]"
	}
	// Source pill — muted vault name so local vs vault provenance is instant.
	sourcePill := ""
	if source != "" && source != "file" {
		short := source
		// strip vw: prefix if present
		if len(short) > 3 && short[:3] == "vw:" {
			short = short[3:]
		}
		sourcePill = fmt.Sprintf(" [#1E293B]%s[-]", tview.Escape(short))
	} else if source == "file" {
		sourcePill = " [#334155]local[-]"
	}
	// Agent hint for vault sessions.
	agent := ""
	if source != "file" && source != "" {
		agent = " [#64748B]•[-] [#0F1E14:#22C55E] Agent: RAM-only [-]"
	}
	left := fmt.Sprintf("%s [#F8FAFC]%s[-] [#64748B](%s)[-]%s [#0F1E14:#22C55E] %s [-] [#64748B]Up: %s[-]%s",
		dot, tview.Escape(alias), tview.Escape(addr), sourcePill, state, upStr, agent)

	// Right: action hints — keep as muted brackets, matches footer violet but here plain for density.
	right := " [#334155]⎘ Copy[-] [#334155]⏻ Disconnect[-] [#7C4D00]Force Kill[-]"
	// Truncate left if terminal narrow — right is secondary.
	text := left + right
	h.view.SetText(text)
}
