package tviewui

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/ac-kurniawan/wardenssh/internal/hosts"
)

// HostListPane is the left pane: a fuzzy filter input + a tview.List of hosts.
// It wraps the existing hosts.List (pure logic) and renders entries with source
// badges and live green dots.
type HostListPane struct {
	hostList   *hosts.List
	filter     *tview.InputField
	list       *tview.List
	flex       *tview.Flex
	onConnect  func(hosts.Entry)
	onScope    func()
	onRefresh  func()
	onCreate   func()
	syncStatus string
	entries    []hosts.Entry // cached visible entries (for SelectedEntry)
}

// NewHostListPane builds the left pane from a hosts.List.
func NewHostListPane(hl *hosts.List) *HostListPane {
	p := &HostListPane{
		hostList: hl,
		filter:   tview.NewInputField(),
		list:     tview.NewList(),
	}
	p.filter.SetLabel("Filter: ").
		SetFieldBackgroundColor(tcell.Color236).
		SetChangedFunc(func(text string) {
			p.hostList.SetFilter(text)
			p.Refresh()
		})

	p.filter.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		return p.handleFilterKey(event)
	})

	p.list.ShowSecondaryText(false).
		SetBorder(true).
		SetTitle(" Hosts ").
		SetTitleAlign(tview.AlignLeft)
	p.list.SetSelectedBackgroundColor(tcell.Color24).
		SetSelectedTextColor(tcell.Color255).
		SetHighlightFullLine(true)

	p.list.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyTab:
			p.hostList.Tab()
			p.Refresh()
			if p.onScope != nil {
				p.onScope()
			}
			return nil
		case tcell.KeyCtrlR:
			p.TriggerRefresh()
			return nil
		case tcell.KeyEnter:
			if p.onConnect != nil {
				if e, ok := p.SelectedEntry(); ok {
					p.onConnect(e)
				}
			}
			return nil
		}
		if event.Rune() == 'r' {
			p.TriggerRefresh()
			return nil
		}
		if event.Rune() == 'n' || event.Rune() == 'a' {
			p.TriggerCreate()
			return nil
		}
		return event
	})

	p.flex = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(p.filter, 1, 0, true).
		AddItem(p.list, 0, 1, false)
	return p
}

func (p *HostListPane) handleFilterKey(event *tcell.EventKey) *tcell.EventKey {
	switch event.Key() {
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
	case tcell.KeyTab:
		p.TabNext()
		if p.onScope != nil {
			p.onScope()
		}
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

// SetOnCreate sets the callback for creating a new connection ('n' / 'a' key).
func (p *HostListPane) SetOnCreate(fn func()) {
	p.onCreate = fn
}

// TriggerCreate fires the create callback (used in tests and shortcuts).
func (p *HostListPane) TriggerCreate() {
	if p.onCreate != nil {
		p.onCreate()
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

// Refresh re-reads the visible entries from the underlying hosts.List and
// rebuilds the tview.List items.
func (p *HostListPane) Refresh() {
	p.entries = p.hostList.Visible()
	p.list.Clear()

	label := scopeLabel(p.hostList.Scope())
	title := fmt.Sprintf(" Hosts (scope: %s) ", label)
	if p.syncStatus != "" {
		title = fmt.Sprintf(" Hosts (scope: %s) • %s ", label, p.syncStatus)
	}
	p.list.SetTitle(title)

	for _, e := range p.entries {
		p.list.AddItem(formatHostLine(e), "", 0, nil)
	}

	if len(p.entries) > 0 {
		p.list.SetCurrentItem(0)
	}
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

// formatHostLine renders one host entry as a list item string with live dot
// and source badge.
func formatHostLine(e hosts.Entry) string {
	liveDot := "  "
	if e.Live {
		liveDot = "[green]●[-] "
	}

	badge := fmt.Sprintf("[gray:black]%s[-]", e.Source)
	if e.AuthKind == "password" {
		badge = fmt.Sprintf("[gray:black]%s [yellow]pw[-][-]", e.Source)
	}

	hostInfo := e.Alias
	if e.HostName != "" && e.HostName != e.Alias {
		hostInfo = fmt.Sprintf("%-25s (%s)", e.Alias, e.HostName)
	}

	return fmt.Sprintf("%s%s %s", liveDot, padRight(hostInfo, 30), badge)
}

func padRight(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}

// FocusFilter moves focus to the filter input field.
func (p *HostListPane) FocusFilter(app *tview.Application) {
	app.SetFocus(p.filter)
}
