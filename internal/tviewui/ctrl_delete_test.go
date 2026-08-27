package tviewui_test

import (
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/ac-kurniawan/wardenssh/internal/hosts"
	"github.com/ac-kurniawan/wardenssh/internal/sshconfig"
	"github.com/ac-kurniawan/wardenssh/internal/tviewui"
)

// ctrlDEvent builds a Ctrl+D key event (the revamp keymap's delete-host
// shortcut in the host pane).
func ctrlDEvent() *tcell.EventKey {
	return tcell.NewEventKey(tcell.KeyCtrlD, 0, tcell.ModNone)
}

// TestApp_CtrlD_OpensDeleteModal: in the host pane, Ctrl+D on a non-live host
// opens the delete confirmation modal (NOT the disconnect modal). The modal's
// target alias must match the selected host.
func TestApp_CtrlD_OpensDeleteModal(t *testing.T) {
	hl := sampleHostList()
	app := tviewui.New(hl, tviewui.Deps{}, nil)
	app.HostPane().Refresh()

	// Host pane focused, no session. prod-db-01 is the first (default-selected)
	// entry and is a non-live file-source host.
	if app.FocusedPane() != "host" {
		t.Fatalf("precondition: FocusedPane = %q, want host", app.FocusedPane())
	}
	if got, _ := app.HostPane().SelectedEntry(); got.Alias != "prod-db-01" {
		t.Fatalf("precondition: SelectedEntry = %q, want prod-db-01", got.Alias)
	}

	app.HandleGlobalKey(ctrlDEvent())

	if !app.InDeleteModal() {
		t.Fatal("Ctrl+D in host pane must open the delete modal")
	}
	if app.InDisconnect() {
		t.Fatal("Ctrl+D must NOT open the disconnect modal")
	}
	dm := app.DeleteModal()
	if dm == nil {
		t.Fatal("expected non-nil DeleteModal after Ctrl+D")
	}
	if got := dm.DeleteTargetAlias(); got != "prod-db-01" {
		t.Errorf("DeleteTargetAlias = %q, want prod-db-01", got)
	}
	if got := dm.DeleteTargetSource(); got != "file" {
		t.Errorf("DeleteTargetSource = %q, want file", got)
	}
}

// TestApp_CtrlD_NoSelection_NoOp: Ctrl+D with no highlighted host entry does
// not open any modal (graceful no-op).
func TestApp_CtrlD_NoSelection_NoOp(t *testing.T) {
	hl := hosts.NewList(nil) // empty list → no selection
	app := tviewui.New(hl, tviewui.Deps{}, nil)
	app.HostPane().Refresh()

	if _, ok := app.HostPane().SelectedEntry(); ok {
		t.Fatal("precondition: empty host list must have no selection")
	}

	app.HandleGlobalKey(ctrlDEvent())

	if app.InDeleteModal() {
		t.Error("Ctrl+D with no selection must not open the delete modal")
	}
	if app.InDisconnect() {
		t.Error("Ctrl+D with no selection must not open the disconnect modal")
	}
}

// TestApp_CtrlD_LiveHost_KillsSessionThenDeletes: Ctrl+D on a host with an
// active session must (1) open the delete modal, (2) on confirm, close the
// session AND remove the config entry. A single confirmation covers both.
func TestApp_CtrlD_LiveHost_KillsSessionThenDeletes(t *testing.T) {
	dir := t.TempDir()
	configPath := dir + "/config"
	if err := sshconfig.AppendHostEntry(configPath, sshconfig.HostConfig{
		Alias: "prod-db-01", HostName: "10.0.0.5",
	}); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	hl := sampleHostList()
	app := tviewui.New(hl, tviewui.Deps{}, nil)
	app.SetSSHConfigPathForTest(configPath)

	key := tviewui.SessionKey("prod-db-01", "file")
	app.TerminalPane().SetSessionForTest(key, "prod-db-01", "file")
	hl.MarkLive("prod-db-01", "file")
	app.HostPane().Refresh() // rebuild cache so SelectedEntry reflects Live=true

	if !app.TerminalPane().HasSession(key) {
		t.Fatal("precondition: live session must be registered")
	}
	if !isLive(hl, "prod-db-01", "file") {
		t.Fatal("precondition: host must be marked live")
	}
	if got, _ := app.HostPane().SelectedEntry(); got.Alias != "prod-db-01" {
		t.Fatalf("precondition: SelectedEntry = %q, want prod-db-01", got.Alias)
	}
	if got, _ := app.HostPane().SelectedEntry(); !got.Live {
		t.Fatal("precondition: SelectedEntry must report Live=true after MarkLive+Refresh")
	}

	app.HandleGlobalKey(ctrlDEvent())

	if !app.InDeleteModal() {
		t.Fatal("Ctrl+D on a live host must open the delete modal")
	}
	// Session and live flag survive while the modal is pending.
	if !app.TerminalPane().HasSession(key) {
		t.Error("session must survive while the delete modal is open")
	}
	if !isLive(hl, "prod-db-01", "file") {
		t.Error("green dot must survive while the delete modal is open")
	}

	// Confirm the delete.
	app.DeleteModal().TriggerDelete()

	// Session killed.
	if app.TerminalPane().HasSession(key) {
		t.Error("expected session closed after deleting a live host")
	}
	if app.TerminalPane().SessionCount() != 0 {
		t.Errorf("SessionCount = %d, want 0", app.TerminalPane().SessionCount())
	}
	// Host entry removed from the in-memory list.
	for _, e := range app.HostList().All() {
		if e.Alias == "prod-db-01" && e.Source == "file" {
			t.Error("expected prod-db-01 removed from host list after delete")
		}
	}
	// Modal dismissed.
	if app.InDeleteModal() {
		t.Error("expected delete modal dismissed after confirm")
	}
}
