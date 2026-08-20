package tviewui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// Design tokens (from .local/plan/tui-revamp.md §4.1). Hex values map 1:1 to
// the spec's palette; tcell degrades to 256/16-color on old terminals.
var (
	AccentColor    = tcell.NewHexColor(0x38BDF8) // active borders, headers
	SelectionBG    = tcell.NewHexColor(0x1E293B) // selected row background
	SelectionFG    = tcell.NewHexColor(0x38BDF8) // selected row foreground
	ConnectedColor = tcell.NewHexColor(0x22C55E) // ● live dot
	IdleColor      = tcell.NewHexColor(0x64748B) // ○ idle dot
	WarningColor   = tcell.NewHexColor(0xF59E0B) // counters, warnings
	InactiveBorder = tcell.NewHexColor(0x334155) // defocused pane borders
	KeyTagColor    = tcell.NewHexColor(0xA855F7) // footer keybinding brackets
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