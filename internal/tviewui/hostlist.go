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
// hosts rendered as a single-row table (no secondary line). Columns are
// alias (flex), user (9ch), host (16ch right), source (8ch right chip).
type HostListPane struct {
	hostList    *hosts.List
	filter      *tview.InputField
	scopeText   *tview.TextView
	header      *tview.TextView
	list        *tview.List
	flex        *tview.Flex
	onConnect   func(hosts.Entry)
	onScope     func()
	onRefresh   func()
	onCreate    func()
	onEdit      func(hosts.Entry)
	syncStatus  string
	focused     bool
	filterFocused bool
	rowWidth    int
	pointerIdx  int
	updating    bool // guard: Refresh ↔ changed-callback recursion
	matchCount  int
	scopeCount  int
	entries     []hosts.Entry // cached visible entries (for SelectedEntry)
	activeKey   string // SessionKey of the currently displayed terminal session (for BG badge)
}

// NewHostListPane builds the left pane from a hosts.List.
func NewHostListPane(hl *hosts.List) *HostListPane {
	p := &HostListPane{
		hostList: hl,
		filter:   tview.NewInputField(),
		scopeText: tview.NewTextView().SetDynamicColors(true).SetTextAlign(tview.AlignRight),
		header:   tview.NewTextView().SetDynamicColors(true).SetTextAlign(tview.AlignLeft),
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
	p.list.SetSecondaryTextColor(InactiveBorder)
	p.list.SetSelectedStyle(tcell.StyleDefault.
		Background(SelectionBG).
		Foreground(SelectionFG).
		Bold(true))
	p.list.SetHighlightFullLine(true)

	// Header: faint uppercase column labels, not selectable. One fixed row
	// above the list, mimics tview.Table header (row 0 fixed) but keeps List
	// navigation clean. Updates on Refresh via updateHeader().
	p.header.SetBorder(false)
	p.header.SetTextColor(InactiveBorder)

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
			bg := p.isBackground(p.entries[old])
			p.list.SetItemText(old, formatHostRowWithBG(p.entries[old], p.rowWidth, false, bg), "")
		}
		p.pointerIdx = index
		bg := p.isBackground(p.entries[index])
		p.list.SetItemText(index, formatHostRowWithBG(p.entries[index], p.rowWidth, true, bg), "")
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

	// Filter card: full-width bordered input (title "🔍 Filter [/]").
	// Scope segmented control sits on its own row below the filter (not
	// side-by-side) so narrow terminals (40cols) still show the filter title
	// and the segmented bar adapts via wrapping/truncation — mirrors
	// index.html's layout where search is row 1 and segmented tabs are row 2.
	p.scopeText.SetTextAlign(tview.AlignLeft)
	filterCard := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(p.filter, 3, 0, true)
	// Scope bar is a single-line TextView below the filter.
	p.scopeText.SetBorder(false)

	// Outer flex stacks filter card + scope bar + header + host list (FlexRow = vertical).
	p.flex = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(filterCard, 3, 0, true).
		AddItem(p.scopeText, 1, 0, false).
		AddItem(p.header, 1, 0, false).
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
	p.filter.SetBorderStyle(style)
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

// SelectedSecondaryText returns the secondary text of the selected item.
// With the table view secondary is always empty (host no longer duplicated).
func (p *HostListPane) SelectedSecondaryText() string {
	idx := p.list.GetCurrentItem()
	if idx < 0 || idx >= p.list.GetItemCount() {
		return ""
	}
	_, sec := p.list.GetItemText(idx)
	return sec
}

// HeaderText returns the current header row text (used in tests).
func (p *HostListPane) HeaderText() string { return p.header.GetText(false) }

// SetRowWidth sets the target row width used by the column formatter (test
// seam; the default 44 matches the plan's host-pane max of 48 minus borders).
func (p *HostListPane) SetRowWidth(width int) {
	p.rowWidth = width
}

// SetActiveKey records the currently displayed terminal session key so
// background sessions (Live but not active) can render the amber BG chip.
func (p *HostListPane) SetActiveKey(key string) { p.activeKey = key }

// ActiveKey returns the currently recorded active session key.
func (p *HostListPane) ActiveKey() string { return p.activeKey }

// isBackground reports whether e is a background session (Live but not active).
func (p *HostListPane) isBackground(e hosts.Entry) bool {
	if !e.Live {
		return false
	}
	if p.activeKey == "" {
		return false
	}
	// SessionKey is alias\x00source; compare with the pane's active key.
	return fmt.Sprintf("%s\x00%s", e.Alias, e.Source) != p.activeKey
}

// Refresh re-reads the visible entries from the underlying hosts.List and
// rebuilds the tview.List items. Each row is a single-line table with
// columns alias | user | host | source. No secondary line — host appears once.
func (p *HostListPane) Refresh() {
	p.entries = p.hostList.Visible()
	p.matchCount = len(p.entries)
	p.scopeCount = p.hostList.CountInScope(p.hostList.Scope())
	p.updateScopeBadge()
	p.updateHeader()
	p.list.Clear()
	p.refreshTitle()

	selected := p.list.GetCurrentItem()
	if selected < 0 {
		selected = 0
	}
	p.pointerIdx = selected
	for i, e := range p.entries {
		bg := p.isBackground(e)
		main := formatHostRowWithBG(e, p.rowWidth, i == selected, bg)
		p.list.AddItem(main, "", 0, nil)
	}
	if len(p.entries) > 0 {
		if selected >= len(p.entries) {
			selected = 0
			p.pointerIdx = 0
		}
		p.list.SetCurrentItem(selected)
	} else if p.hostList.Scope() != "" || p.filter.GetText() != "" {
		// Empty filtered state — keep list bordered but hint at next action.
		p.list.SetTitle(fmt.Sprintf(" Hosts (scope: %s) — no matches • press / to filter, Ctrl+N new ", scopeLabel(p.hostList.Scope())))
	}
}

// formatHostSecondary is kept for backward compatibility but the table view
// has no secondary line (host would be duplicated). Returns empty to ensure
// host appears exactly once in the primary row.
func formatHostSecondary(_ hosts.Entry) string { return "" }

// updateScopeBadge renders the segmented scope control next to the filter,
// mirroring index-tui.html's tab row: "All 4  personal ●  work ●  local"
// The active scope is highlighted with SelectionBG+Accent, inactive muted.
// Appends "· N matches" with amber counter for filter feedback.
func (p *HostListPane) updateScopeBadge() {
	scopes := p.hostList.Scopes()
	if len(scopes) == 0 {
		p.scopeText.SetText(fmt.Sprintf("[#38BDF8]Scope: %s (%d)[-] [#64748B]·[-] [#F59E0B]%d matches[-]",
			p.ScopeLabel(), p.scopeCount, p.matchCount))
		return
	}
	var parts []string
	current := p.hostList.Scope()
	for _, sc := range scopes {
		label := shortScopeLabel(sc)
		count := p.hostList.CountInScope(sc)
		// Display label: "All" for "" else short label
		if sc == current {
			parts = append(parts, fmt.Sprintf("[#38BDF8:#1E293B:b] %s %d [-]", tview.Escape(label), count))
		} else {
			parts = append(parts, fmt.Sprintf("[#64748B] %s %d [-]", tview.Escape(label), count))
		}
	}
	seg := strings.Join(parts, " ")
	// Append matches counter if filtered or many entries
	seg += fmt.Sprintf(" [#64748B]·[-] [#F59E0B]%d matches[-]", p.matchCount)
	p.scopeText.SetText(seg)
}

// updateHeader renders the faint column header row above the list.
// Columns: (pointer/glyph) alias | user | host | source — muted #64748B.
func (p *HostListPane) updateHeader() {
	p.header.SetText(formatHeader(p.rowWidth))
}

func formatHeader(width int) string {
	// Mirror row column widths.
	addrW := 16
	userW := 9
	sourceW := 8
	showSource := true
	if width < 60 {
		showSource = false
		sourceW = 0
	}
	if width < 50 {
		userW = 6
	}
	prefixW := 4 // pointer(2)+glyph(1)+space(1)
	nameW := width - prefixW - 1 - userW - 1 - addrW
	if showSource {
		nameW -= 1 + sourceW
	}
	if nameW < 4 {
		nameW = 4
	}
	prefix := "    " // blank where pointer+glyph would be
	aliasLabel := padRight(truncateEllipsis("alias", nameW), nameW)
	userLabel := padRight(truncateEllipsis("user", userW), userW)
	hostLabel := padLeft(truncateEllipsis("host", addrW), addrW)
	line := prefix + aliasLabel + " " + "[#64748B]" + tview.Escape(userLabel) + "[-]" + " " + "[#64748B]" + tview.Escape(hostLabel) + "[-]"
	if showSource {
		srcLabel := padLeft("source", sourceW)
		line += " " + "[#64748B]" + tview.Escape(srcLabel) + "[-]"
	}
	return line
}

func shortScopeLabel(scope string) string {
	switch scope {
	case "":
		return "All"
	case "file":
		return "local"
	default:
		// strip vw: prefix
		return strings.TrimPrefix(scope, "vw:")
	}
}

// refreshTitle rebuilds the list title from the current scope and sync status.
func (p *HostListPane) refreshTitle() {
	label := scopeLabel(p.hostList.Scope())
	title := fmt.Sprintf(" Hosts (scope: %s) ", label)
	if p.syncStatus != "" {
		title = fmt.Sprintf(" Hosts (scope: %s) • %s ", label, p.syncStatus)
	}
	if len(p.entries) == 0 && p.filter.GetText() == "" && p.syncStatus == "" {
		// Hint count when unfiltered but empty (e.g. fresh install, no hosts yet).
		title = fmt.Sprintf(" Hosts (scope: %s) — 0 hosts • Ctrl+N new ", label)
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

// FormatHostRowWithBGForTest exposes the BG-aware formatter (tests only).
func FormatHostRowWithBGForTest(e hosts.Entry, width int, selected bool, isBackground bool) string {
	return formatHostRowWithBG(e, width, selected, isBackground)
}

// formatHostRow renders one host entry as a fixed-width, column-aligned row
func formatHostRow(e hosts.Entry, width int, selected bool) string {
	return formatHostRowWithBG(e, width, selected, false)
}

// formatHostRowWithBG is the BG-aware variant: when isBackground is true the
// entry is live but not the currently displayed session — rendered with an
// amber [orange]BG[-] chip. The row is a single-line table:
// pointer glyph alias(+pw/BG) user host source
// Host appears exactly once (right-aligned host column, no secondary line).
func formatHostRowWithBG(e hosts.Entry, width int, selected bool, isBackground bool) string {
	pointer := "  "
	if selected {
		pointer = GlyphPointer + " "
	}
	glyph := GlyphIdle
	if e.Live {
		glyph = GlyphConnected
	}
	prefix := pointer + glyph + " "

	addrW := 16 // IPv4 = 15 cols, Tailscale domain = 16
	userW := 9
	sourceW := 8
	showSource := true
	if width < 60 {
		showSource = false
		sourceW = 0
	}
	if width < 50 {
		userW = 6
	}
	nameW := width - runewidth.StringWidth(prefix) - 1 - userW - 1 - addrW
	if showSource {
		nameW -= 1 + sourceW
	}
	if nameW < 4 {
		nameW = 4
	}
	// Build suffix badges: pw (yellow) + BG (amber) — both foreground-only.
	// Visual suffix is " pw" / " BG" (3ch each) — tags are not counted for width.
	suffixTags := ""
	suffixVisual := ""
	if e.AuthKind == "password" {
		suffixTags += " [yellow]pw[-]"
		suffixVisual += " pw"
	}
	if isBackground {
		suffixTags += " [orange]BG[-]"
		suffixVisual += " BG"
	}
	suffixVisualLen := runewidth.StringWidth(suffixVisual)

	var aliasPart string
	if suffixTags != "" {
		aliasMax := nameW - suffixVisualLen
		if aliasMax < 1 {
			aliasMax = 1
		}
		aliasTrunc := truncateEllipsis(e.Alias, aliasMax)
		// Tagged aliasPart: truncated alias + colored badges
		aliasPart = aliasTrunc + suffixTags
		// Pad visual width to nameW: visual = aliasTrunc + suffixVisual
		visualW := runewidth.StringWidth(aliasTrunc) + suffixVisualLen
		if visualW < nameW {
			aliasPart += strings.Repeat(" ", nameW-visualW)
		}
	} else {
		aliasPart = truncateEllipsis(e.Alias, nameW)
		aliasPart = padRight(aliasPart, nameW)
	}

	// User column: left-aligned, muted, "—" if empty.
	userPlain := e.User
	if userPlain == "" {
		userPlain = "—"
	}
	if runewidth.StringWidth(userPlain) > userW {
		userPlain = runewidth.Truncate(userPlain, userW, "…")
	}
	userPlain = padRight(userPlain, userW)
	userCell := fmt.Sprintf("[#94A3B8]%s[-]", tview.Escape(userPlain))

	// Host column: right-aligned, never clips IPv4.
	hostPlain := e.HostName
	if runewidth.StringWidth(hostPlain) > addrW {
		hostPlain = runewidth.Truncate(hostPlain, addrW, "…")
	}
	hostPlain = padLeft(hostPlain, addrW)
	hostCell := tview.Escape(hostPlain)

	// Source chip: personal (sky) or local (slate), right-aligned.
	var parts []string
	parts = append(parts, prefix+aliasPart, userCell, hostCell)
	if showSource {
		src := e.Source
		label := ""
		if src == "file" {
			label = "local"
		} else {
			label = strings.TrimPrefix(src, "vw:")
			if label == "" {
				label = src
			}
		}
		srcPlain := label
		if runewidth.StringWidth(srcPlain) > sourceW {
			srcPlain = runewidth.Truncate(srcPlain, sourceW, "…")
		}
		srcPlain = padLeft(srcPlain, sourceW)
		var srcCell string
		if src == "file" {
			srcCell = fmt.Sprintf("[#94A3B8]%s[-]", tview.Escape(srcPlain))
		} else {
			srcCell = fmt.Sprintf("[#38BDF8]%s[-]", tview.Escape(srcPlain))
		}
		parts = append(parts, srcCell)
	}
	line := strings.Join(parts, " ")
	return padRight(line, width)
}

func containsBadge(s, badge string) bool {
	return strings.Contains(s, badge)
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
