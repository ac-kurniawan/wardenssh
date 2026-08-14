// Package tviewui: footer.go renders a one-line hotkey hint at the bottom of
// the launcher, switching between host-list and terminal modes as focus moves.
package tviewui

import "github.com/rivo/tview"

// Footer is a single-line hotkey hint bar rendered at the bottom of the
// launcher. It has two modes that mirror the current pane focus.
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

// SetMode updates the hint text: "host" shows host-list bindings, anything
// else shows the terminal bindings.
func (f *Footer) SetMode(mode string) {
	switch mode {
	case "terminal":
		f.view.SetText("[gray:black]Ctrl+B list · Ctrl+C SIGINT · Esc list[-]")
	default:
		f.view.SetText("[gray:black]Tab scope · Ctrl+N new · d delete · Ctrl+R sync · Enter connect · Esc/q quit[-]")
	}
}

// Text returns the rendered footer text (used in tests).
func (f *Footer) Text() string { return f.view.GetText(true) }
