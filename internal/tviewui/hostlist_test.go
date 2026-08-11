package tviewui_test

import (
	"strings"
	"testing"

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
