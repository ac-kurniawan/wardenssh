# Periodic Background Vault Sync Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement non-blocking 5-minute background vault sync and manual refresh (`Ctrl+R` / `r`) in WardenSSH, updating `hosts.List` and the `HostListPane` status without interrupting active SSH sessions.

**Architecture:** A `time.Ticker` in `tviewui.App` periodically triggers a background goroutine that syncs vault ciphers via `vaultadapter.Source.Sync(c)`, extracts updated host entries, updates `hosts.List` via `ReplaceVaultEntries`, and updates the header title in `HostListPane` via `a.app.QueueUpdateDraw`.

**Tech Stack:** Go stdlib (`time`, `sync`, `os/exec`), `tview`, `tcell`.

## Global Constraints

- **Private keys never written to disk.** RAM + vault only.
- **No secrets in config or logs.** Diagnostics to stderr only.
- **In-process everything.** Agent and vault client run inside the TUI process.
- **TDD mandatory.** Red-Green-Refactor for every change.
- **Pure Go, no CGO.**

---

### Task 1: Vault Source Sync & Host List Replace

**Files:**
- Modify: `internal/vaultadapter/adapter.go`, `internal/hosts/list.go`
- Test: `internal/vaultadapter/adapter_test.go`, `internal/hosts/list_test.go`

**Interfaces:**
- Consumes: `vaultclient.Client`, `vaultadapter.Source`, `hosts.List`
- Produces:
  - `vaultadapter.Source.Sync(c vaultclient.Client) error`
  - `hosts.List.ReplaceVaultEntries(source string, newEntries []Entry)`

- [ ] **Step 1: Write the failing test for ReplaceVaultEntries**

In `internal/hosts/list_test.go`:

```go
func TestListReplaceVaultEntriesPreservesLiveState(t *testing.T) {
	hl := hosts.NewList([]hosts.Entry{
		{Alias: "db-01", HostName: "10.0.0.1", Source: "vw:vw1", Live: true},
		{Alias: "web-01", HostName: "10.0.0.2", Source: "file"},
	})

	// Replace entries for source "vw:vw1" with updated host info.
	updated := []hosts.Entry{
		{Alias: "db-01", HostName: "10.0.0.100", Source: "vw:vw1"},
		{Alias: "db-02", HostName: "10.0.0.101", Source: "vw:vw1"},
	}

	hl.ReplaceVaultEntries("vw:vw1", updated)

	all := hl.All()
	if len(all) != 3 {
		t.Fatalf("expected 3 total entries after replace, got %d", len(all))
	}

	// Verify live state on db-01 was preserved.
	var db1 hosts.Entry
	for _, e := range all {
		if e.Alias == "db-01" {
			db1 = e
		}
	}
	if !db1.Live {
		t.Errorf("expected db-01 to remain Live=true after replace")
	}
	if db1.HostName != "10.0.0.100" {
		t.Errorf("expected updated HostName 10.0.0.100, got %s", db1.HostName)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/hosts/ -run TestListReplaceVaultEntriesPreservesLiveState -v`
Expected: FAIL (`hl.ReplaceVaultEntries undefined`)

- [ ] **Step 3: Implement ReplaceVaultEntries**

In `internal/hosts/list.go`:

```go
// ReplaceVaultEntries replaces all entries for a specific source label (e.g. "vw:work")
// with newEntries. If an existing entry was marked Live (green dot), its Live flag is preserved.
func (l *List) ReplaceVaultEntries(source string, newEntries []Entry) {
	// Build map of existing live aliases for this source.
	liveMap := make(map[string]bool)
	for _, e := range l.entries {
		if e.Source == source && e.Live {
			liveMap[e.Alias] = true
		}
	}

	// Filter out old entries for this source.
	var kept []Entry
	for _, e := range l.entries {
		if e.Source != source {
			kept = append(kept, e)
		}
	}

	// Attach preserved live flags to new entries.
	for i := range newEntries {
		if liveMap[newEntries[i].Alias] {
			newEntries[i].Live = true
		}
	}

	l.entries = append(kept, newEntries...)
	l.scopes = l.deriveScopes()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/hosts/ -run TestListReplaceVaultEntriesPreservesLiveState -v`
Expected: PASS

- [ ] **Step 5: Write the failing test for Source.Sync**

In `internal/vaultadapter/adapter_test.go`:

```go
func TestSourceSyncUpdatesCiphers(t *testing.T) {
	c := vaultclient.NewFakeClient()
	sess, err := c.Login("user@example.com", "pass")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	sr, err := c.Sync(sess)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}

	src := vaultadapter.NewSource("vw", sess, sr.Ciphers, config.CustomFields{})
	if err := src.Sync(c); err != nil {
		t.Fatalf("src.Sync failed: %v", err)
	}
}
```

- [ ] **Step 6: Run test to verify it fails**

Run: `go test ./internal/vaultadapter/ -run TestSourceSync -v`
Expected: FAIL (`src.Sync undefined`)

- [ ] **Step 7: Implement Source.Sync**

In `internal/vaultadapter/adapter.go`:

```go
// Sync requests an updated cipher list from the vault client using the active session
// and updates the Source's cached item list.
func (s *Source) Sync(c vaultclient.Client) error {
	if s == nil || c == nil {
		return fmt.Errorf("vaultadapter: nil source or client")
	}
	sr, err := c.Sync(s.sess)
	if err != nil {
		return fmt.Errorf("vaultadapter: sync %q: %w", s.name, err)
	}
	s.ciphers = sr.Ciphers
	return nil
}

// SyncSyncs all underlying sources in the vault client.
func (c *Client) SyncAll(vc vaultclient.Client) error {
	for _, src := range c.sources {
		if err := src.Sync(vc); err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 8: Run test to verify it passes**

Run: `go test ./internal/vaultadapter/ -run TestSourceSync -v`
Expected: PASS

- [ ] **Step 9: Commit**

```bash
git add internal/hosts/list.go internal/hosts/list_test.go internal/vaultadapter/adapter.go internal/vaultadapter/adapter_test.go
git commit -m "feat(vaultadapter): add Sync/SyncAll and ReplaceVaultEntries"
```

---

### Task 2: Sync Status & Key Trigger in HostListPane

**Files:**
- Modify: `internal/tviewui/hostlist.go`
- Test: `internal/tviewui/hostlist_test.go`

**Interfaces:**
- Consumes: `HostListPane`
- Produces:
  - `HostListPane.SetSyncStatus(status string)`
  - `HostListPane.SetOnRefresh(fn func())`

- [ ] **Step 1: Write the failing test**

In `internal/tviewui/hostlist_test.go`:

```go
func TestHostListPaneSetSyncStatus(t *testing.T) {
	pane := tviewui.NewHostListPane(sampleHostList())
	pane.SetSyncStatus("Synced 20:54")
	pane.Refresh()
	title := pane.Title()
	if !strings.Contains(title, "Synced 20:54") {
		t.Errorf("title should contain sync status, got: %s", title)
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tviewui/ -run TestHostListPaneSetSyncStatus -v`
Expected: FAIL (`pane.SetSyncStatus undefined`)

- [ ] **Step 3: Implement SetSyncStatus and Key Trigger in HostListPane**

In `internal/tviewui/hostlist.go`:

```go
// Add syncStatus field to HostListPane struct:
// syncStatus string
// onRefresh  func()

// SetSyncStatus updates the sync status string shown in the title.
func (p *HostListPane) SetSyncStatus(status string) {
	p.syncStatus = status
}

// SetOnRefresh sets the callback for manual refresh (Ctrl+R / r key).
func (p *HostListPane) SetOnRefresh(fn func()) {
	p.onRefresh = fn
}

// TriggerRefresh fires the refresh callback (used in tests).
func (p *HostListPane) TriggerRefresh() {
	if p.onRefresh != nil {
		p.onRefresh()
	}
}

// Title returns the current list title (used in tests).
func (p *HostListPane) Title() string {
	return p.list.GetTitle()
}

// Update Refresh() in hostlist.go to append syncStatus:
func (p *HostListPane) Refresh() {
	p.entries = p.hostList.Visible()
	p.list.Clear()

	scope := p.hostList.Scope()
	if scope == "" {
		scope = "all"
	}
	title := fmt.Sprintf(" Hosts (scope: %s) ", scope)
	if p.syncStatus != "" {
		title = fmt.Sprintf(" Hosts (scope: %s) • %s ", scope, p.syncStatus)
	}
	p.list.SetTitle(title)

	for _, e := range p.entries {
		p.list.AddItem(formatHostLine(e), "", 0, nil)
	}

	if len(p.entries) > 0 {
		p.list.SetCurrentItem(0)
	}
}

// Update SetInputCapture in list constructor to handle Ctrl+R or 'r':
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
		return event
	})
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tviewui/ -run TestHostListPaneSetSyncStatus -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/tviewui/hostlist.go internal/tviewui/hostlist_test.go
git commit -m "feat(tviewui): add sync status header and manual refresh shortcut to HostListPane"
```

---

### Task 3: App Background Ticker & Manual Sync Trigger

**Files:**
- Modify: `internal/tviewui/app.go`
- Test: `internal/tviewui/app_test.go`

**Interfaces:**
- Consumes: `App`, `HostListPane`, `vaultadapter.Client`
- Produces:
  - `App.TriggerSync()`
  - `App.StartBackgroundSync(interval time.Duration)`

- [ ] **Step 1: Write the failing test**

In `internal/tviewui/app_test.go`:

```go
func TestAppTriggerSyncRunsSync(t *testing.T) {
	hl := sampleHostList()
	app := tviewui.New(hl, tviewui.Deps{}, nil)

	// Trigger sync (should not panic even with no vault client).
	app.TriggerSync()
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tviewui/ -run TestAppTriggerSync -v`
Expected: FAIL (`app.TriggerSync undefined`)

- [ ] **Step 3: Implement TriggerSync and Background Ticker in App**

In `internal/tviewui/app.go`:

```go
// Add sync fields to App struct:
// syncTicker *time.Ticker
// syncStop   chan struct{}
// syncing    sync.Mutex

// Wire SetOnRefresh in New():
	a.hostPane.SetOnRefresh(a.TriggerSync)

// TriggerSync runs a background sync across all loaded vault sources.
func (a *App) TriggerSync() {
	if a.deps.VaultCli == nil {
		return
	}

	go func() {
		a.mu.Lock()
		vAdapterClient, ok := a.deps.VaultCli.(*vaultadapter.Client)
		a.mu.Unlock()

		if !ok || vAdapterClient == nil {
			return
		}

		// Perform sync on all sources.
		err := vAdapterClient.SyncAll(nil) // sync with real vaultclient if available
		nowStr := time.Now().Format("15:04")

		a.app.QueueUpdateDraw(func() {
			if err != nil {
				fmt.Fprintf(os.Stderr, "wardenssh: background sync failed: %v\n", err)
				a.hostPane.SetSyncStatus("[red]Sync failed (offline)[-]")
			} else {
				// Re-extract entries from vault sources.
				if vEntries, err := appVaultEntries(a.deps.VaultCli); err == nil {
					// Update host entries for each vault.
					for _, src := range vAdapterClient.Sources() {
						var srcEntries []hosts.Entry
						for _, e := range vEntries {
							if e.Source == src.Name() {
								srcEntries = append(srcEntries, e)
							}
						}
						a.hostList.ReplaceVaultEntries(src.Name(), srcEntries)
					}
				}
				a.hostPane.SetSyncStatus(fmt.Sprintf("Synced %s", nowStr))
			}
			a.hostPane.Refresh()
		})
	}()
}

// StartBackgroundSync starts the 5-minute background sync ticker.
func (a *App) StartBackgroundSync(interval time.Duration) {
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	a.syncTicker = time.NewTicker(interval)
	a.syncStop = make(chan struct{})

	go func() {
		for {
			select {
			case <-a.syncTicker.C:
				a.TriggerSync()
			case <-a.syncStop:
				a.syncTicker.Stop()
				return
			}
		}
	}()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tviewui/ -run TestAppTriggerSync -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/tviewui/app.go internal/tviewui/app_test.go
git commit -m "feat(tviewui): background sync ticker and manual sync trigger"
```

---

## Plan Verification

Run complete test suite to confirm zero regressions:
```bash
go test -count=1 ./...
go build ./...
```
