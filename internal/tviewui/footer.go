// Package tviewui: footer.go renders a two-line hotkey hint bar at the bottom
// of the launcher, switching between host-list and terminal modes as focus
// moves. Key names are wrapped in violet `[#A855F7]` tags (§4.1 KeyTag).
package tviewui

import "github.com/rivo/tview"

// Footer is a two-line hotkey hint bar rendered at the bottom of the launcher.
// Line 1 carries context-aware bindings for the focused pane; line 2 the
// global bindings. It has two modes that mirror the current pane focus.
type Footer struct {
	view *tview.TextView
}

// NewFooter builds a Footer defaulting to host-list mode hints.
func NewFooter() *Footer {
	v := tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignLeft)
	f := &Footer{view: v}
	f.SetMode("host")
	return f
}

// Primitive returns the tview primitive for layout embedding.
func (f *Footer) Primitive() tview.Primitive { return f.view }

// keyTag wraps a key name in a violet key tag. The bracket contents are escaped
// so tview's tag parser does not swallow key labels like "[j]"/"[/]".
func keyTag(k string) string {
	return "[#A855F7]" + tview.Escape(k) + "[-]"
}

// SetMode updates the hint text: "host" shows host-list bindings, anything
// else shows the terminal bindings. Global line is shared.
func (f *Footer) SetMode(mode string) {
	global := keyTag("[Ctrl+S]") + " Scopes  " + keyTag("[?]") + " Help  " + keyTag("[/]") + " Filter  " + keyTag("[Ctrl+R]") + " Sync  " + keyTag("[Ctrl+Q]") + " Quit"
	if mode == "terminal" {
		f.view.SetText(
			keyTag("[Ctrl+\\]") + "/" + keyTag("[Ctrl+B]") + " Sidebar  " + keyTag("[Ctrl+Shift+C]") + " Copy  " + keyTag("[Ctrl+D]") + " Disconnect\n" + global)
		return
	}
	f.view.SetText(
		keyTag("[↑/↓]/[j]/[k]") + " Select  " + keyTag("[Enter]") + " Connect  " + keyTag("[Tab]/[Ctrl+B]") + " Terminal  " + keyTag("[Ctrl+N]") + " New  " + keyTag("[Ctrl+E]") + " Edit\n" + global)
}

// Text returns the rendered footer text (used in tests).
func (f *Footer) Text() string { return f.view.GetText(true) }

// RawText returns the footer text with style tags intact (used in tests).
func (f *Footer) RawText() string { return f.view.GetText(false) }