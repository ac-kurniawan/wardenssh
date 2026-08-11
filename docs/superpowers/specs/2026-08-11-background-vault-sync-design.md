# Design Spec: Periodic Background Vault Sync

**Date:** 2026-08-11
**Feature:** Background Vault Sync & Manual Refresh (`Ctrl+R`)

## 1. Overview
WardenSSH currently performs a single sync with BitWarden/VaultWarden during initial vault unlock (`SetupModal`). This spec introduces a periodic non-blocking background sync ticker (every 5 minutes) and a manual sync shortcut (`Ctrl+R` / `r`). Sync runs in a background goroutine and updates `hosts.List` without interrupting active SSH sessions or UI interactions.

## 2. Architecture & Components

### 2.1 Sync Engine (`internal/vaultadapter`)
- `Source` in `vaultadapter` holds the current `vaultclient.Session` and decrypted cipher list (`[]vaultclient.CipherItem`).
- Add `Sync(c vaultclient.Client) error` method to `vaultadapter.Source` (and `vaultadapter.Client`) that calls `c.Sync(sess)` and updates the cached ciphers.
- Add `ReplaceVaultEntries(sourceName string, entries []Entry)` to `hosts.List` to update vault entries for a given vault source while preserving active `Live` session flags (`●`).

### 2.2 App Integration (`internal/tviewui/app.go`)
- In `App`, start a 5-minute `time.Ticker` goroutine when vault setup completes (or when app starts with active vault clients).
- Add `TriggerSync()` to `App` for manual sync (`Ctrl+R` or `r` in host list).
- Prevent concurrent sync runs using a `syncing` guard flag.
- On sync completion:
  - If successful: update `hosts.List`, set status string to `Synced HH:MM`, and call `app.QueueUpdateDraw(hostPane.Refresh)`.
  - If failed: log diagnostic to `stderr`, set status string to `Sync failed [offline]`, and call `app.QueueUpdateDraw(hostPane.Refresh)`.

### 2.3 UI Header Status (`internal/tviewui/hostlist.go`)
- Update `HostListPane` to display the current sync status in the list border title:
  - Example: `Hosts (scope: all) • Synced 20:54`
  - Offline example: `Hosts (scope: all) • Sync failed [offline]`

## 3. Key Handling (`Ctrl+R` / `r`)
- In `HostListPane` key capture, `Ctrl+R` or `r` (when list is focused) invokes the sync trigger callback to start a manual sync.

## 4. Testing Strategy
- Unit test `vaultadapter.Source.Sync` with fake/mock `vaultclient`.
- Unit test `hosts.List.ReplaceVaultEntries` verifying live dots and scope preservation.
- Unit test `tviewui.App` background sync ticker and manual sync trigger.
