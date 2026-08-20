package tviewui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// HelpModal is the '?' overlay: a context-aware keybinding sheet. Host mode
// lists the host-pane bindings; terminal mode the embedded-terminal bindings.
// Esc/Enter closes.
type HelpModal struct {
	help    *tview.TextView
	onClose func()
}

// NewHelpModal builds the help sheet for the given context ("host" or
// "terminal").
func NewHelpModal(mode string) *HelpModal {
	m := &HelpModal{}
	m.help = tview.NewTextView().SetDynamicColors(true).SetTextAlign(tview.AlignLeft)
	m.help.SetBorder(true)
	m.help.SetTitle(" Keyboard Shortcuts ")
	m.help.SetTitleAlign(tview.AlignLeft)
	m.help.SetText(helpText(mode))
	m.help.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape || event.Key() == tcell.KeyEnter {
			m.triggerClose()
			return nil
		}
		return event
	})
	return m
}

// helpText returns the keymap listing for a context. Key labels are wrapped in
// violet tags; bracket contents are escaped so tview's tag parser does not
// swallow "[j]"/"[/]" etc.
func helpText(mode string) string {
	key := func(k string) string {
		return "[#A855F7]" + tview.Escape(k) + "[-]"
	}
	host := key("[↑/↓] / [j] / [k]") + " Select host\n" +
		key("[Enter]") + " Connect / switch session\n" +
		key("[Tab] / [Ctrl+B]") + " Focus terminal PTY\n" +
		key("[Ctrl+S]") + "   Scope switcher (groups)\n" +
		key("[/]") + "       Focus filter search\n" +
		key("[Ctrl+N]") + "     New connection\n" +
		key("[Ctrl+E]") + "     Edit connection\n" +
		key("[Delete]") + "    Delete connection\n" +
		key("[Ctrl+R]") + "     Sync vault\n" +
		key("[?]") + "         This help\n" +
		key("[Ctrl+Q]") + "     Quit WardenSSH"
	terminal := key("[Ctrl+\\] / [Ctrl+B]") + " Focus sidebar (host list)\n" +
		key("[Ctrl+S]") + "   Scope switcher (groups)\n" +
		key("[Ctrl+Shift+C]") + " Copy selection\n" +
		key("[Ctrl+C]") + "    Copy / SIGINT\n" +
		key("[Ctrl+D]") + "    Disconnect session\n" +
		key("[?]") + "          This help\n" +
		key("[Ctrl+Q]") + "    Quit WardenSSH"
	if mode == "terminal" {
		return terminal
	}
	return host
}

// Primitive returns the tview primitive for layout embedding.
func (m *HelpModal) Primitive() tview.Primitive { return m.help }

// Text returns the rendered help text (used in tests).
func (m *HelpModal) Text() string { return m.help.GetText(true) }

// SetOnClose installs the close callback.
func (m *HelpModal) SetOnClose(fn func()) { m.onClose = fn }

// TriggerClose fires the close callback (tests / Esc/Enter).
func (m *HelpModal) TriggerClose() { m.triggerClose() }

func (m *HelpModal) triggerClose() {
	if m.onClose != nil {
		m.onClose()
	}
}