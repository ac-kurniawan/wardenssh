package tviewui

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/mattn/go-runewidth"
	"github.com/rivo/tview"

	"github.com/ac-kurniawan/wardenssh/internal/hosts"
)

// HostListPane is the left pane: a bounded fuzzy filter card + a tview.List of
// hosts. It wraps the existing hosts.List (pure logic) and renders entries with
// status glyphs, a selection pointer, and a guaranteed-width address column.
type HostListPane struct {
	hostList   *hosts.List
	filter     *tview.InputField
	scopeText  *tview.TextView
	list       *tview.List
	flex       *tview.Flex
	onConnect func(hosts.Entry)
	onScope   func()
	onRefresh func()
	onCreate  func()
	onEdit    func(hosts.Entry)
	syncStatus string
	focused    bool
	filterFocused bool
	rowWidth   int
	pointerIdx int
	updating   bool // guard: Refresh ↔ changed-callback recursion
	matchCount int
	scopeCount int
	entries    []hosts.Entry // cached visible entries (for SelectedEntry)
}

// NewHostListPane builds the left pane from a hosts.List.
func NewHostListPane(hl *hosts.List) *HostListPane {
	p := &HostListPane{
		hostList: hl,
		filter:   tview.NewInputField(),
		scopeText: tview.NewTextView().SetDynamicColors(true).SetTextAlign(tview.AlignRight),
		list:     tview.NewList(),
		rowWidth: 44,
	}
	p.filter.SetLabel("> ").
		SetFieldBackgroundColor(tcell.Color236).
		SetChangedFunc(func(text string) {
			p.hostList.SetFilter(text)
			p.Refresh()
		})
	// Border + title belong to the Box superclass; set them separately so the
	// chain returns InputField where needed (SetBorder returns *Box).
	p.filter.SetBorder(true)
	p.filter.SetTitle(" 🔍 Filter [/] ")
	p.filter.SetTitleAlign(tview.AlignLeft)

	p.filter.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		return p.handleFilterKey(event)
	})

	p.list.ShowSecondaryText(false).
		SetBorder(true).
		SetTitle(" Hosts ").
		SetTitleAlign(tview.AlignLeft)
	p.list.SetSelectedStyle(tcell.StyleDefault.
		Background(SelectionBG).
		Foreground(SelectionFG).
		Bold(true))
	p.list.SetHighlightFullLine(true)

	// Move the pointer glyph to the newly selected row on navigation. Only the
	// previously- and newly-selected rows are re-rendered (SetItemText does not
	// fire a "changed" event, so no recursion).
	p.list.SetChangedFunc(func(index int, _ string, _ string, _ rune) {
		if p.updating || index < 0 || index >= len(p.entries) {
			return
		}
		p.updating = true
		old := p.pointerIdx
		if old >= 0 && old < len(p.entries) && old != index {
			p.list.SetItemText(old, formatHostRow(p.entries[old], p.rowWidth, false), "")
		}
		p.pointerIdx = index
		p.list.SetItemText(index, formatHostRow(p.entries[index], p.rowWidth, true), "")
		p.updating = false
	})

	p.list.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyCtrlR:
			p.TriggerRefresh()
			return nil
		case tcell.KeyCtrlN:
			p.TriggerCreate()
			return nil
		case tcell.KeyCtrlE:
			p.TriggerEdit()
			return nil
		case tcell.KeyEnter:
			if p.onConnect != nil {
				if e, ok := p.SelectedEntry(); ok {
					p.onConnect(e)
				}
			}
			return nil
		}
		if event.Rune() == 'j' {
			cur := p.list.GetCurrentItem()
			if cur < p.list.GetItemCount()-1 {
				p.list.SetCurrentItem(cur + 1)
			}
			return nil
		}
		if event.Rune() == 'k' {
			cur := p.list.GetCurrentItem()
			if cur > 0 {
				p.list.SetCurrentItem(cur - 1)
			}
			return nil
		}
		if event.Rune() == 'r' || event.Rune() == 'R' {
			p.TriggerRefresh()
			return nil
		}
		return event
	})

	// Filter card: bordered input (title "🔍 Filter [/]") + right-aligned scope
	// badge with the per-scope host count. The badge sits to the RIGHT of the
	// input (FlexColumn = one item per column = horizontal arrangement), so the
	// fixed-width badge never consumes the input's height. The badge lives
	// outside the input's border so the input keeps its own rounded frame.
	filterCard := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(p.filter, 0, 1, true).
		AddItem(p.scopeText, 20, 0, false)

	// Outer flex stacks the filter card above the host list (FlexRow = one
	// item per row = vertical arrangement).
	p.flex = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(filterCard, 3, 0, true).
		AddItem(p.list, 0, 1, false)
	return p
}

func (p *HostListPane) handleFilterKey(event *tcell.EventKey) *tcell.EventKey {
	switch event.Key() {
	case tcell.KeyCtrlN:
		p.TriggerCreate()
		return nil
	case tcell.KeyCtrlE:
		p.TriggerEdit()
		return nil
	case tcell.KeyDown:
		cur := p.list.GetCurrentItem()
		if cur < p.list.GetItemCount()-1 {
			p.list.SetCurrentItem(cur + 1)
		}
		return nil
	case tcell.KeyUp:
		cur := p.list.GetCurrentItem()
		if cur > 0 {
			p.list.SetCurrentItem(cur - 1)
		}
		return nil
	case tcell.KeyEnter:
		p.TriggerConnect()
		return nil
	case tcell.KeyCtrlR:
		p.TriggerRefresh()
		return nil
	case tcell.KeyEscape:
		if p.filter.GetText() != "" {
			p.SetFilter("")
			p.Refresh()
			return nil
		}
	}
	return event
}

// HandleFilterKey simulates filter key input (used in tests).
func (p *HostListPane) HandleFilterKey(event *tcell.EventKey) *tcell.EventKey {
	return p.handleFilterKey(event)
}

// HandleListKey routes a key event through the list's input handler (tests).
func (p *HostListPane) HandleListKey(event *tcell.EventKey) *tcell.EventKey {
	if h := p.list.InputHandler(); h != nil {
		h(event, func(tview.Primitive) {})
	}
	return event
}

// FilterFocused reports whether the filter input currently owns focus.
func (p *HostListPane) FilterFocused() bool { return p.filterFocused }

// Primitive returns the tview primitive for layout embedding.
func (p *HostListPane) Primitive() tview.Primitive { return p.flex }

// SetOnConnect installs the callback fired when Enter is pressed on a host.
func (p *HostListPane) SetOnConnect(fn func(hosts.Entry)) { p.onConnect = fn }

// SetOnScopeChange installs the callback fired when Tab cycles scope.
func (p *HostListPane) SetOnScopeChange(fn func()) { p.onScope = fn }

// SetSyncStatus updates the sync status string shown in the title.
func (p *HostListPane) SetSyncStatus(status string) {
	p.syncStatus = status
}

// MatchCount returns the number of visible (filtered) entries in scope.
func (p *HostListPane) MatchCount() int { return p.matchCount }

// ScopeCount returns the total number of launchable entries in the current
// scope, ignoring the filter (scope badge counter).
func (p *HostListPane) ScopeCount() int { return p.scopeCount }

// ScopeLabel returns the friendly current-scope label (e.g. "~/.ssh/config").
func (p *HostListPane) ScopeLabel() string { return scopeLabel(p.hostList.Scope()) }

// FilterTitle returns the filter card's border title (used in tests).
func (p *HostListPane) FilterTitle() string { return p.filter.GetTitle() }

// SetFocused updates the pane's border color to reflect keyboard focus:
// accent when focused, inactive otherwise (focus is color-coded, never
// line-weight — see ApplyRoundedBorders).
func (p *HostListPane) SetFocused(focused bool) {
	p.focused = focused
	if !focused {
		p.filterFocused = false
	}
	style := tcell.StyleDefault.Foreground(InactiveBorder)
	if focused {
		style = tcell.StyleDefault.Foreground(AccentColor)
	}
	p.list.SetBorderStyle(style)
	p.refreshTitle()
}

// BorderColor returns the current list border color (used in tests).
func (p *HostListPane) BorderColor() tcell.Color { return p.list.GetBorderColor() }

// SetOnRefresh sets the callback for manual refresh (Ctrl+R / r key).
func (p *HostListPane) SetOnRefresh(fn func()) {
	p.onRefresh = fn
}

// TriggerRefresh fires the refresh callback (used in tests and shortcuts).
func (p *HostListPane) TriggerRefresh() {
	if p.onRefresh != nil {
		p.onRefresh()
	}
}

// SetOnCreate sets the callback for creating a new connection (Ctrl+N).
func (p *HostListPane) SetOnCreate(fn func()) {
	p.onCreate = fn
}

// TriggerCreate fires the create callback (used in tests and shortcuts).
func (p *HostListPane) TriggerCreate() {
	if p.onCreate != nil {
		p.onCreate()
	}
}

// SetOnEdit sets the callback for editing a connection (Ctrl+E).
func (p *HostListPane) SetOnEdit(fn func(hosts.Entry)) {
	p.onEdit = fn
}

// TriggerEdit fires the edit callback for the currently selected entry.
func (p *HostListPane) TriggerEdit() {
	if p.onEdit != nil {
		if e, ok := p.SelectedEntry(); ok {
			p.onEdit(e)
		}
	}
}

// Title returns the current list title (used in tests).
func (p *HostListPane) Title() string {
	return p.list.GetTitle()
}

// SetFilter sets the filter text programmatically (used in tests).
func (p *HostListPane) SetFilter(text string) {
	p.filter.SetText(text)
	p.hostList.SetFilter(text)
}

// FilterText returns the current filter input text.
func (p *HostListPane) FilterText() string { return p.filter.GetText() }

// TabNext advances the scope cycle (wraps around).
func (p *HostListPane) TabNext() {
	p.hostList.Tab()
	p.Refresh()
}

// CurrentScope returns the current scope label.
func (p *HostListPane) CurrentScope() string {
	return p.hostList.Scope()
}

// SetScope sets the current scope label (programmatically, for tests).
func (p *HostListPane) SetScope(s string) {
	p.hostList.SetScope(s)
}

// TriggerConnect fires the connect callback for the currently selected entry.
// Used in tests to avoid simulating key events.
func (p *HostListPane) TriggerConnect() {
	if p.onConnect != nil {
		if e, ok := p.SelectedEntry(); ok {
			p.onConnect(e)
		}
	}
}

// SelectedEntry returns the currently highlighted host entry.
func (p *HostListPane) SelectedEntry() (hosts.Entry, bool) {
	idx := p.list.GetCurrentItem()
	if idx < 0 || idx >= len(p.entries) {
		return hosts.Entry{}, false
	}
	return p.entries[idx], true
}

// SelectedRenderText returns the rendered text of the currently selected list
// item (for test assertions on badges/green dots).
func (p *HostListPane) SelectedRenderText() string {
	idx := p.list.GetCurrentItem()
	if idx < 0 || idx >= p.list.GetItemCount() {
		return ""
	}
	main, _ := p.list.GetItemText(idx)
	return main
}

// SetRowWidth sets the target row width used by the column formatter (test
// seam; the default 44 matches the plan's host-pane max of 48 minus borders).
func (p *HostListPane) SetRowWidth(width int) {
	p.rowWidth = width
}

// Refresh re-reads the visible entries from the underlying hosts.List and
// rebuilds the tview.List items. The selected row gets the pointer glyph.
func (p *HostListPane) Refresh() {
	p.entries = p.hostList.Visible()
	p.matchCount = len(p.entries)
	p.scopeCount = p.hostList.CountInScope(p.hostList.Scope())
	p.updateScopeBadge()
	p.list.Clear()
	p.refreshTitle()

	selected := p.list.GetCurrentItem()
	if selected < 0 {
		selected = 0
	}
	p.pointerIdx = selected
	for i, e := range p.entries {
		p.list.AddItem(formatHostRow(e, p.rowWidth, i == selected), "", 0, nil)
	}
	if len(p.entries) > 0 {
		if selected >= len(p.entries) {
			selected = 0
			p.pointerIdx = 0
		}
		p.list.SetCurrentItem(selected)
	}
}

// updateScopeBadge renders the right-aligned scope badge next to the filter:
// "Scope: <label> (<count>)" with the match counter in amber.
func (p *HostListPane) updateScopeBadge() {
	label := p.ScopeLabel()
	p.scopeText.SetText(fmt.Sprintf("[#38BDF8]Scope: %s (%d)[-]  [#F59E0B]%d matches[-]",
		label, p.scopeCount, p.matchCount))
}

// refreshTitle rebuilds the list title from the current scope and sync status.
func (p *HostListPane) refreshTitle() {
	label := scopeLabel(p.hostList.Scope())
	title := fmt.Sprintf(" Hosts (scope: %s) ", label)
	if p.syncStatus != "" {
		title = fmt.Sprintf(" Hosts (scope: %s) • %s ", label, p.syncStatus)
	}
	p.list.SetTitle(title)
}

// scopeLabel maps a raw source label to a human-friendly display name:
// "" (all) -> "all", "file" -> "~/.ssh/config", anything else (a vault) is
// shown as-is (the vault name, e.g. "vw").
func scopeLabel(scope string) string {
	switch scope {
	case "":
		return "all"
	case "file":
		return "~/.ssh/config"
	default:
		return scope
	}
}

// FormatHostRowForTest exposes the row formatter (tests only).
func FormatHostRowForTest(e hosts.Entry, width int, selected bool) string {
	return formatHostRow(e, width, selected)
}

// formatHostRow renders one host entry as a fixed-width, column-aligned row:
//
//	{pointer}{glyph} {name:truncated…} {address:right-aligned}
//
// The address column is high-priority: it never truncates; the name column
// absorbs the width deficit with a tail ellipsis. Style tags are foreground-
// only so tview's selected-row background fills the full line (defect #2).
func formatHostRow(e hosts.Entry, width int, selected bool) string {
	pointer := "  "
	if selected {
		pointer = GlyphPointer + " "
	}
	glyph := GlyphIdle
	if e.Live {
		glyph = GlyphConnected
	}
	addrW := 16 // IPv4 = 15 cols, Tailscale domain = 16 (§5.2)
	nameW := width - runewidth.StringWidth(pointer) - 1 - 1 - addrW
	if nameW < 4 {
		nameW = 4
	}
	name := truncateEllipsis(e.Alias, nameW)
	if e.AuthKind == "password" {
		pw := " [yellow]pw[-]"
		name = truncateEllipsis(e.Alias + pw, nameW)
	}
	addr := e.HostName
	if runewidth.StringWidth(addr) > addrW {
		addr = runewidth.Truncate(addr, addrW, "…")
	}
	addr = padLeft(addr, addrW)
	line := pointer + glyph + " " + name + " " + addr
	return padRight(line, width)
}

func truncateEllipsis(s string, maxW int) string {
	if runewidth.StringWidth(s) <= maxW {
		return s
	}
	if maxW <= 1 {
		return ""
	}
	return runewidth.Truncate(s, maxW-1, "…")
}

func padLeft(s string, width int) string {
	w := runewidth.StringWidth(s)
	if w >= width {
		return s
	}
	return strings.Repeat(" ", width-w) + s
}

func padRight(s string, width int) string {
	if runewidth.StringWidth(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-runewidth.StringWidth(s))
}

// FocusFilter moves focus to the filter input field and records it (the flag
// makes '/'-focus testable without a running tview app).
func (p *HostListPane) FocusFilter(app *tview.Application) {
	p.filterFocused = true
	if app != nil {
		app.SetFocus(p.filter)
	}
}
