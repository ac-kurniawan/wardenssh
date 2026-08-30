package tviewui

import (
	"fmt"
	"strings"

	"github.com/rivo/tview"
)

// Version is the display version shown in the top bar (injected at build
// time via ldflags or set from main.version at startup). Defaults to "dev"
// for local builds; goreleaser injects the git tag (e.g. "v0.2.4").
var Version = "dev"

// TopBar is the header strip at the very top of the app — mirrors
// index-tui.html's bar: "🛡 warden ssh  •  vault synced • RAM only  •  session count".
// It is a single-line TextView with dynamic colors, updated by App whenever
// sync status or session count changes.
type TopBar struct {
	view       *tview.TextView
	syncStatus string
	sessCount  int
	bgCount    int
	vaultNames []string
}

// NewTopBar builds the header bar with the given vault names (for the pill row).
func NewTopBar(vaultNames []string) *TopBar {
	v := tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignLeft)
	v.SetBorder(false)
	b := &TopBar{view: v, vaultNames: vaultNames}
	b.refresh()
	return b
}

// Primitive returns the tview primitive for layout embedding.
func (b *TopBar) Primitive() tview.Primitive { return b.view }

// Text returns the rendered header text (tests).
func (b *TopBar) Text() string { return b.view.GetText(true) }

// RawText returns the header text with tags intact (tests).
func (b *TopBar) RawText() string { return b.view.GetText(false) }

// SetSyncStatus updates the vault sync indicator ("Synced 15:04" or
// "Sync failed (offline)") and redraws.
func (b *TopBar) SetSyncStatus(s string) {
	b.syncStatus = s
	b.refresh()
}

// SetSessionCounts updates the active/background counters.
func (b *TopBar) SetSessionCounts(active, total int) {
	b.sessCount = active
	if total >= active {
		b.bgCount = total - active
	} else {
		b.bgCount = 0
	}
	b.refresh()
}

func (b *TopBar) refresh() {
	syncText := b.syncStatus
	if syncText == "" {
		syncText = "[#64748B]vault sync…[-]"
	} else if syncText == "Synced" || len(syncText) > 6 && syncText[:6] == "Synced" {
		syncText = fmt.Sprintf("[#22C55E]● %s[-]", syncText)
	} else if syncText == "Sync failed (offline)" || syncText == "[red]Sync failed (offline)[-]" {
		syncText = "[#EF4444]○ offline[-]"
	} else {
		syncText = fmt.Sprintf("[#64748B]%s[-]", tview.Escape(syncText))
	}

	// Vault pills — each vault name as a pill; highlight when we have sessions
	vaultPills := ""
	for _, n := range b.vaultNames {
		vaultPills += fmt.Sprintf(" [#1E293B]%s[-]", tview.Escape(n))
	}
	if vaultPills == "" {
		vaultPills = " [#334155]~/.ssh/config[-]"
	}

	sessionHint := ""
	if b.sessCount > 0 || b.bgCount > 0 {
		if b.bgCount > 0 {
			sessionHint = fmt.Sprintf(" [#22C55E]● %d active[-] [#F59E0B]◌ %d bg[-]", b.sessCount, b.bgCount)
		} else {
			sessionHint = fmt.Sprintf(" [#22C55E]● %d active[-]", b.sessCount)
		}
	}

	// RAM-only badge is always visible — the security guarantee from README.
	ramBadge := "[#0F1E14:#22C55E] RAM-only [-]"
	v := Version
	if v != "dev" && !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	versionTag := fmt.Sprintf(" [#64748B]%s • Zero-Disk[-]", tview.Escape(v))

	text := fmt.Sprintf("[#F8FAFC]🛡 warden[-][#22C55E]ssh[-]%s [#64748B]│[-] %s [#64748B]│[-] %s%s%s",
		versionTag, syncText, ramBadge, sessionHint, vaultPills)
	b.view.SetText(text)
}
