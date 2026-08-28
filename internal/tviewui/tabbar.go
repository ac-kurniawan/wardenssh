package tviewui

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/mattn/go-runewidth"
	"github.com/rivo/tview"
)

// SessionTabBar is the horizontal tab strip above the terminal — one tab per
// live session. The active session has a green dot and SelectionBG highlight;
// background sessions have an amber dot. Mirrors index-tui.html's tab row:
// "● shopee-2 20.198… ×" etc.
type SessionTabBar struct {
	flex     *tview.Flex
	tabs     []*tview.TextView
	keys     []string
	aliases  map[string]string
	hosts    map[string]string
	active   string
	onSelect func(key string)
	onClose  func(key string)
}

// NewSessionTabBar builds an empty tab bar.
func NewSessionTabBar() *SessionTabBar {
	b := &SessionTabBar{
		flex:    tview.NewFlex().SetDirection(tview.FlexColumn),
		aliases: map[string]string{},
		hosts:   map[string]string{},
	}
	return b
}

// Primitive returns the tview primitive for layout embedding (height 1).
func (b *SessionTabBar) Primitive() tview.Primitive { return b.flex }

// SetOnSelect installs the callback fired when a tab is clicked/selected.
func (b *SessionTabBar) SetOnSelect(fn func(key string)) { b.onSelect = fn }

// SetOnClose installs the callback fired when a tab's close is requested.
func (b *SessionTabBar) SetOnClose(fn func(key string)) { b.onClose = fn }

// Visible reports whether the tab bar has any tabs.
func (b *SessionTabBar) Visible() bool { return len(b.keys) > 0 }

// Update rebuilds the tabs from the given ordered session keys. active is the
// key of the currently displayed session. aliases/hosts map each key to its
// display alias and hostname for the tab label.
func (b *SessionTabBar) Update(keys []string, active string, aliases, hosts map[string]string) {
	b.keys = append([]string(nil), keys...)
	b.active = active
	for k, v := range aliases {
		b.aliases[k] = v
	}
	for k, v := range hosts {
		b.hosts[k] = v
	}
	b.rebuild()
}

// TabCount returns the number of tabs (tests).
func (b *SessionTabBar) TabCount() int { return len(b.keys) }

// ActiveKey returns the active tab key (tests).
func (b *SessionTabBar) ActiveKey() string { return b.active }

func (b *SessionTabBar) rebuild() {
	b.flex.Clear()
	b.tabs = nil
	for _, key := range b.keys {
		alias := b.aliases[key]
		if alias == "" {
			alias = key
		}
		host := b.hosts[key]
		label := formatTabLabel(alias, host, key == b.active)
		tv := tview.NewTextView().
			SetDynamicColors(true).
			SetTextAlign(tview.AlignCenter)
		tv.SetText(label)
		tv.SetBorder(false)
		// Active tab: SelectionBG + accent border color + bold.
		if key == b.active {
			tv.SetBackgroundColor(SelectionBG)
			tv.SetTextColor(AccentColor)
		} else {
			tv.SetBackgroundColor(tcell.ColorDefault)
			// Background sessions get amber dot (via label color tag).
		}
		// Capture key for click handler
		k := key
		tv.SetMouseCapture(func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
			if action == tview.MouseLeftClick {
				if b.onSelect != nil {
					b.onSelect(k)
				}
				return action, nil
			}
			return action, event
		})
		b.tabs = append(b.tabs, tv)
		b.flex.AddItem(tv, 0, 1, false)
	}
}

func formatTabLabel(alias, host string, active bool) string {
	dot := "[#F59E0B]●[-]" // amber for background
	if active {
		dot = "[#22C55E]●[-]"
	}
	// Truncate host to keep tab compact (mirrors index-tui.html "20.198…").
	h := host
	if runewidth.StringWidth(h) > 12 {
		h = runewidth.Truncate(h, 11, "…")
	}
	if h != "" {
		return fmt.Sprintf(" %s %s %s ", dot, tview.Escape(alias), tview.Escape(h))
	}
	return fmt.Sprintf(" %s %s ", dot, tview.Escape(alias))
}

// TabLabelsForTest returns the plain tab labels (without tags) for assertions.
func (b *SessionTabBar) TabLabelsForTest() []string {
	var out []string
	for _, k := range b.keys {
		alias := b.aliases[k]
		if alias == "" {
			alias = k
		}
		out = append(out, strings.TrimSpace(alias))
	}
	return out
}

// HandleKey routes navigation: Tab cycles tabs, Ctrl+W closes active (optional).
func (b *SessionTabBar) HandleKey(event *tcell.EventKey) *tcell.EventKey {
	if len(b.keys) == 0 {
		return event
	}
	switch event.Key() {
	case tcell.KeyTab:
		// Cycle to next tab
		idx := -1
		for i, k := range b.keys {
			if k == b.active {
				idx = i
				break
			}
		}
		next := (idx + 1) % len(b.keys)
		if b.onSelect != nil {
			b.onSelect(b.keys[next])
		}
		return nil
	}
	if event.Key() == tcell.KeyCtrlW {
		if b.onClose != nil && b.active != "" {
			b.onClose(b.active)
		}
		return nil
	}
	return event
}
