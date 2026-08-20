package tviewui_test

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/ac-kurniawan/wardenssh/internal/hosts"
	"github.com/ac-kurniawan/wardenssh/internal/tviewui"
)

func sampleHostList() *hosts.List {
	return hosts.NewList([]hosts.Entry{
		{Alias: "prod-db-01", HostName: "10.0.0.5", Source: "file"},
		{Alias: "web-02", HostName: "web.internal", Source: "file"},
		{Alias: "ci-box", HostName: "10.1.0.10", Source: "vw:personal"},
	})
}

func TestHostListPaneShowsEntries(t *testing.T) {
	pane := tviewui.NewHostListPane(sampleHostList())
	pane.Refresh()
	entry, ok := pane.SelectedEntry()
	if !ok {
		t.Fatal("expected a selected entry")
	}
	if entry.Alias != "prod-db-01" {
		t.Errorf("first entry = %q, want prod-db-01", entry.Alias)
	}
}

func TestHostListPaneFilterNarrows(t *testing.T) {
	pane := tviewui.NewHostListPane(sampleHostList())
	pane.SetFilter("prod")
	pane.Refresh()
	entry, ok := pane.SelectedEntry()
	if !ok {
		t.Fatal("expected a selected entry after filter")
	}
	if entry.Alias != "prod-db-01" {
		t.Errorf("filtered entry = %q, want prod-db-01", entry.Alias)
	}
}

func TestHostListPaneTabCyclesScope(t *testing.T) {
	pane := tviewui.NewHostListPane(sampleHostList())
	before := pane.CurrentScope()
	pane.TabNext()
	after := pane.CurrentScope()
	if before == after {
		t.Errorf("Tab did not advance scope: before=%q after=%q", before, after)
	}
}

// TestHostListPaneTitleShowsFriendlyScope: the list title shows a friendly
// scope label — "all" for all sources, "~/.ssh/config" for the file source.
func TestHostListPaneTitleShowsFriendlyScope(t *testing.T) {
	pane := tviewui.NewHostListPane(sampleHostList())
	pane.Refresh()
	if !strings.Contains(pane.Title(), "all") {
		t.Errorf("title = %q, want it to contain 'all'", pane.Title())
	}

	pane.SetScope("file")
	pane.Refresh()
	if !strings.Contains(pane.Title(), "~/.ssh/config") {
		t.Errorf("title = %q, want it to contain '~/.ssh/config'", pane.Title())
	}
}

func TestHostListPaneConnectCallback(t *testing.T) {
	pane := tviewui.NewHostListPane(sampleHostList())
	pane.Refresh()
	var got hosts.Entry
	pane.SetOnConnect(func(e hosts.Entry) {
		got = e
	})
	pane.TriggerConnect()
	if got.Alias != "prod-db-01" {
		t.Errorf("connect callback got = %q, want prod-db-01", got.Alias)
	}
}

func TestHostListPaneLiveDot(t *testing.T) {
	hl := sampleHostList()
	hl.MarkLive("prod-db-01", "file")
	pane := tviewui.NewHostListPane(hl)
	pane.Refresh()
	text := pane.SelectedRenderText()
	if !strings.Contains(text, "●") {
		t.Errorf("expected green dot in rendered text, got: %s", text)
	}
}

// TestHostListPanePasswordBadge: a password-credential host renders a "pw"
// marker in its source badge.
func TestHostListPanePasswordBadge(t *testing.T) {
	hl := hosts.NewList([]hosts.Entry{
		{Alias: "prod-db", HostName: "10.0.0.9", Source: "vw:personal", AuthKind: "password"},
		{Alias: "ci-box", HostName: "10.1.0.10", Source: "vw:personal"},
	})
	pane := tviewui.NewHostListPane(hl)
	pane.Refresh()
	text := pane.SelectedRenderText()
	if !strings.Contains(text, "pw") {
		t.Errorf("password host badge missing 'pw' in %q", text)
	}
}

func TestHostListPaneSetSyncStatus(t *testing.T) {
	pane := tviewui.NewHostListPane(sampleHostList())
	pane.SetSyncStatus("Synced 20:54")
	pane.Refresh()
	title := pane.Title()
	if !strings.Contains(title, "Synced 20:54") {
		t.Errorf("title should contain sync status, got: %s", title)
	}
}

func TestHostRowNeverClipsAddress(t *testing.T) {
	// IPv4 (15 cols) and a 16-col Tailscale domain must survive intact; the
	// NAME takes the truncation hit (defect #3 regression).
	for _, addr := range []string{"104.250.1.2", "gitlab.corp.net"} {
		row := tviewui.FormatHostRowForTest(hosts.Entry{
			Alias: "gitlab-infrastructure-prod", HostName: addr,
			Source: "file",
		}, 34, false)
		if !strings.Contains(row, addr) {
			t.Errorf("address %q clipped in %q", addr, row)
		}
	}
}

func TestHostRowTailEllipsisOnName(t *testing.T) {
	row := tviewui.FormatHostRowForTest(hosts.Entry{Alias: "gitlab-infrastructure-prod", HostName: "10.0.0.1", Source: "file"}, 34, false)
	if !strings.Contains(row, "10.0.0.1") {
		t.Errorf("ip missing: %q", row)
	}
	if !strings.Contains(row, "…") {
		t.Errorf("expected ellipsis in %q", row)
	}
}

func TestHostRowSelectedPointer(t *testing.T) {
	row := tviewui.FormatHostRowForTest(hosts.Entry{Alias: "a", HostName: "1.2.3.4", Source: "file"}, 34, true)
	if !strings.HasPrefix(row, tviewui.GlyphPointer) {
		t.Errorf("selected row must start with pointer, got %q", row)
	}
	row2 := tviewui.FormatHostRowForTest(hosts.Entry{Alias: "a", HostName: "1.2.3.4", Source: "file"}, 34, false)
	if strings.HasPrefix(row2, tviewui.GlyphPointer) {
		t.Errorf("unselected row must not start with pointer, got %q", row2)
	}
}

func TestHostRowStatusGlyphs(t *testing.T) {
	live := tviewui.FormatHostRowForTest(hosts.Entry{Alias: "a", HostName: "1.2.3.4", Source: "file", Live: true}, 34, false)
	if !strings.Contains(live, tviewui.GlyphConnected) {
		t.Errorf("live row missing ●: %q", live)
	}
	idle := tviewui.FormatHostRowForTest(hosts.Entry{Alias: "a", HostName: "1.2.3.4", Source: "file"}, 34, false)
	if !strings.Contains(idle, tviewui.GlyphIdle) {
		t.Errorf("idle row missing ○: %q", idle)
	}
}

func TestHostRowNoBackgroundTags(t *testing.T) {
	row := tviewui.FormatHostRowForTest(hosts.Entry{Alias: "a", HostName: "1.2.3.4", Source: "file", Live: true}, 34, true)
	if strings.Contains(row, ":black") {
		t.Errorf("row must not contain background style tags (selection artifact): %q", row)
	}
}

func TestHostListPaneRefreshCallbackTriggered(t *testing.T) {
	pane := tviewui.NewHostListPane(sampleHostList())
	refreshed := false
	pane.SetOnRefresh(func() {
		refreshed = true
	})
	pane.TriggerRefresh()
	if !refreshed {
		t.Fatal("expected OnRefresh callback after TriggerRefresh")
	}
}

func TestFilterInputCaptureNavigatesAndConnects(t *testing.T) {
	pane := tviewui.NewHostListPane(sampleHostList())
	pane.Refresh()

	// Initially entry index 0 (prod-db-01) is selected.
	entry, _ := pane.SelectedEntry()
	if entry.Alias != "prod-db-01" {
		t.Fatalf("expected initial selected entry prod-db-01, got %s", entry.Alias)
	}

	// Move down via filter input capture.
	pane.HandleFilterKey(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone))
	entry, _ = pane.SelectedEntry()
	if entry.Alias != "web-02" {
		t.Errorf("expected selected entry after KeyDown web-02, got %s", entry.Alias)
	}

	// Connect on Enter via filter input capture.
	var connected hosts.Entry
	pane.SetOnConnect(func(e hosts.Entry) {
		connected = e
	})
	pane.HandleFilterKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	if connected.Alias != "web-02" {
		t.Errorf("expected connect callback for web-02 on Enter, got %s", connected.Alias)
	}
}

func TestHostListPaneCreateCallback(t *testing.T) {
	pane := tviewui.NewHostListPane(sampleHostList())
	created := false
	pane.SetOnCreate(func() {
		created = true
	})
	pane.TriggerCreate()
	if !created {
		t.Fatal("expected OnCreate callback after TriggerCreate")
	}
}

func TestFilterInputCaptureCtrlNTriggersCreate(t *testing.T) {
	pane := tviewui.NewHostListPane(sampleHostList())
	created := false
	pane.SetOnCreate(func() {
		created = true
	})
	pane.HandleFilterKey(tcell.NewEventKey(tcell.KeyCtrlN, 0, tcell.ModNone))
	if !created {
		t.Fatal("expected OnCreate callback when Ctrl+N pressed in filter")
	}
}

func TestHostListPaneEditCallback(t *testing.T) {
	pane := tviewui.NewHostListPane(sampleHostList())
	pane.Refresh()
	var edited hosts.Entry
	pane.SetOnEdit(func(e hosts.Entry) {
		edited = e
	})
	pane.TriggerEdit()
	if edited.Alias != "prod-db-01" {
		t.Errorf("expected OnEdit callback for prod-db-01, got %q", edited.Alias)
	}
}

func TestFilterInputCaptureCtrlETriggersEdit(t *testing.T) {
	pane := tviewui.NewHostListPane(sampleHostList())
	pane.Refresh()
	var edited hosts.Entry
	pane.SetOnEdit(func(e hosts.Entry) {
		edited = e
	})
	pane.HandleFilterKey(tcell.NewEventKey(tcell.KeyCtrlE, 0, tcell.ModNone))
	if edited.Alias != "prod-db-01" {
		t.Errorf("expected OnEdit callback for prod-db-01 when Ctrl+E pressed in filter, got %q", edited.Alias)
	}
}



