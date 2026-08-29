package tviewui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// Design tokens — single source of truth for all UI chrome (v2 manifest §1).
// AccentColor is the focused border / selected-row foreground (#38BDF8 sky);
// ConnectedColor is the live-session dot only (#22C55E green); WarningColor
// is the amber counter / BG chip (#F59E0B); InactiveBorder is the defocused
// border (#334155). No raw hex outside this file — grep tcell.NewHexColor is gated.
// Hex values map 1:1 to the spec's palette; tcell degrades to 256/16-color on old terminals.
var (
	AccentColor    = tcell.NewHexColor(0x38BDF8) // active borders, headers, selected fg
	SelectionBG    = tcell.NewHexColor(0x1E293B) // selected row background
	SelectionFG    = tcell.NewHexColor(0x38BDF8) // selected row foreground (== Accent)
	ConnectedColor = tcell.NewHexColor(0x22C55E) // ● live dot (green — not the focus color)
	IdleColor      = tcell.NewHexColor(0x64748B) // ○ idle dot
	WarningColor   = tcell.NewHexColor(0xF59E0B) // counters, warnings, BG chip (amber)
	InactiveBorder = tcell.NewHexColor(0x334155) // defocused pane borders
	KeyTagColor    = tcell.NewHexColor(0xA855F7) // footer keybinding brackets (violet)
)

// Status glyphs (§4.2). ◌ (connecting) and ▲ (unreachable) are deferred — the
// data model only exposes Live bool.
const (
	GlyphConnected = "●"
	GlyphIdle      = "○"
	GlyphPointer   = "▶"
)

// ApplyRoundedBorders switches the global tview border glyph set to rounded
// Unicode (╭─╮ │ ╰─╯) for both normal AND focus states. Focus is expressed via
// border color (see focus-aware pane borders), never via line weight — this
// fixes the single-vs-double border mismatch between panes.
func ApplyRoundedBorders() {
	tview.Borders.Horizontal = '─'
	tview.Borders.Vertical = '│'
	tview.Borders.TopLeft = '╭'
	tview.Borders.TopRight = '╮'
	tview.Borders.BottomLeft = '╰'
	tview.Borders.BottomRight = '╯'
	tview.Borders.LeftT = '├'
	tview.Borders.RightT = '┤'
	tview.Borders.TopT = '┬'
	tview.Borders.BottomT = '┴'
	tview.Borders.Cross = '┼'
	tview.Borders.HorizontalFocus = tview.Borders.Horizontal
	tview.Borders.VerticalFocus = tview.Borders.Vertical
	tview.Borders.TopLeftFocus = tview.Borders.TopLeft
	tview.Borders.TopRightFocus = tview.Borders.TopRight
	tview.Borders.BottomLeftFocus = tview.Borders.BottomLeft
	tview.Borders.BottomRightFocus = tview.Borders.BottomRight
}