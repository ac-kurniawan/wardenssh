package tviewui_test

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/ac-kurniawan/wardenssh/internal/tviewui"
)

func TestThemeTokenValues(t *testing.T) {
	if got := tviewui.AccentColor; got != tcell.NewHexColor(0x38BDF8) {
		t.Errorf("AccentColor = %v, want 0x38BDF8", got)
	}
	if got := tviewui.SelectionBG; got != tcell.NewHexColor(0x1E293B) {
		t.Errorf("SelectionBG = %v, want 0x1E293B", got)
	}
	if got := tviewui.ConnectedColor; got != tcell.NewHexColor(0x22C55E) {
		t.Errorf("ConnectedColor = %v, want 0x22C55E", got)
	}
	if got := tviewui.IdleColor; got != tcell.NewHexColor(0x64748B) {
		t.Errorf("IdleColor = %v, want 0x64748B", got)
	}
	if got := tviewui.InactiveBorder; got != tcell.NewHexColor(0x334155) {
		t.Errorf("InactiveBorder = %v, want 0x334155", got)
	}
	if got := tviewui.KeyTagColor; got != tcell.NewHexColor(0xA855F7) {
		t.Errorf("KeyTagColor = %v, want 0xA855F7", got)
	}
}

func TestApplyRoundedBorders(t *testing.T) {
	tviewui.ApplyRoundedBorders()
	if tview.Borders.TopLeft != '╭' || tview.Borders.TopRight != '╮' ||
		tview.Borders.BottomLeft != '╰' || tview.Borders.BottomRight != '╯' {
		t.Errorf("corners not rounded: %q %q %q %q",
			tview.Borders.TopLeft, tview.Borders.TopRight,
			tview.Borders.BottomLeft, tview.Borders.BottomRight)
	}
	if tview.Borders.Horizontal != '─' || tview.Borders.Vertical != '│' {
		t.Errorf("edges not light: %q %q", tview.Borders.Horizontal, tview.Borders.Vertical)
	}
	// Focus glyphs must equal normal glyphs — focus is color-coded, not double-line.
	if tview.Borders.HorizontalFocus != tview.Borders.Horizontal ||
		tview.Borders.TopLeftFocus != tview.Borders.TopLeft {
		t.Errorf("focus glyphs must match normal glyphs (no double-line on focus)")
	}
}

func TestGlyphConstants(t *testing.T) {
	if tviewui.GlyphConnected != "●" || tviewui.GlyphIdle != "○" || tviewui.GlyphPointer != "▶" {
		t.Errorf("glyphs wrong: %q %q %q", tviewui.GlyphConnected, tviewui.GlyphIdle, tviewui.GlyphPointer)
	}
}