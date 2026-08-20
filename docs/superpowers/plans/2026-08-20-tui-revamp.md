# TUI Revamp (Design Tokens, Geometry, Ergonomics) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Transform the WardenSSH tview-based split-pane TUI from the functional prototype into an ergonomic, visually balanced terminal UI (lazygit/k9s-class): rounded borders, focus-aware colors, columnar host rows without truncation glitches, a bounded filter header with match counters, a Ctrl+B scope switcher modal, a help sheet, a redesigned keymap, and a polished terminal pane title.

**Architecture:** Adapt `.local/plan/tui-revamp.md` (which was written against Bubble Tea/Ratatui blueprints) to the **actual stack in this repo: tview + tcell + tvxterm**. All visual tokens are centralized in a new `theme.go`; the global `tview.Borders` struct is switched to rounded glyphs (focus indicated by color, not double-lines); `HostListPane` renders runewidth-aware columns (status glyph + pointer + name + right-aligned address) with full-line selection; a bordered filter card carries scope badge + match counter; new `ScopeModal` + `HelpModal` are wired via the revamped keymap (Tab→terminal, Ctrl+\→sidebar, Ctrl+B→scope, /→filter, ?→help, Ctrl+Q→quit, Ctrl+D→disconnect). PTY isolation is **already satisfied** by tvxterm's in-memory VT emulator — no work there.

**Tech Stack:** Go 1.26, `github.com/rivo/tview` (List/InputField/Modal/TextView/Flex), `github.com/gdamore/tcell/v2` (`NewHexColor`, border styles), `github.com/mattn/go-runewidth` (width-aware truncation), existing `internal/hosts` / `internal/tviewui` packages.

## Global Constraints

- TDD mandatory: failing test → minimal implementation → refactor; every commit passes `go test ./...`.
- Private keys/tokens never logged or written; no new secrets anywhere.
- Style tags in list rows must be foreground-only (`[green]●[-]`) — a `:black` background tag survives tview's selected style and creates the gray artifact defect.
- The revamp keymap replaces legacy bindings: Tab no longer cycles scope (Ctrl+B modal does), Ctrl+B no longer toggles panes (Tab / Ctrl+\ do), Ctrl+D becomes disconnect (delete moves to Delete key only).
- Left:right pane proportions stay 1:2 (33%/67% — inside the plan's 30–35% / 65–70% band); no responsive min/max clamping in v0 (tview Flex has no per-draw min-width; documented deviation).
- Ping telemetry (ICMP) is **deferred** (privileged, platform-specific, no data-model support). Session uptime is implemented.
- Connnecting (◌) / unreachable (▲) glyphs are **deferred** — the data model only has `Live bool`; v0 renders ● (live) / ○ (idle).

---

### Task 1: Theme tokens + rounded borders (`theme.go`)

**Why:** All visual tokens in one place; rounded borders app-wide via the mutable global `tview.Borders` struct; focus glyphs equal to normal glyphs so focus is conveyed by **color**, not double-lines (defect #4).

**Files:**
- Create: `internal/tviewui/theme.go`
- Test: `internal/tviewui/theme_test.go`

**Interfaces:**
- Produces: package-level color values `AccentColor`, `SelectionBG`, `SelectionFG`, `ConnectedColor`, `IdleColor`, `WarningColor`, `InactiveBorder`, `KeyTagColor` (all `tcell.Color` via `NewHexColor`), glyph constants `GlyphConnected = "●"`, `GlyphIdle = "○"`, `GlyphPointer = "▶"`, and `ApplyRoundedBorders()` which mutates `tview.Borders`.

- [ ] **Step 1: Write the failing test**

`internal/tviewui/theme_test.go`:
```go
package tviewui_test

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/ac-kurniawan/wardenssh/internal/tviewui"
)

func TestThemeTokenValues(t *testing.T) {
	if got := tviewui.AccentColor; got != tcell.NewHexColor(0x38BDF8) {
		t.Errorf("AccentColor = %v, want 0x38BDF8", got)
	}
	if got := tviewui.SelectionBG; got != tcell.NewHexColor(0x1E293B) {
		t.Errorf("SelectionBG = %v, want 0x1E293B", got)
	}
	if got := tviewui.ConnectedColor; got != tcell.NewHexColor(0x22C55E) {
		t.Errorf("ConnectedColor = %v, want 0x22C55E", got)
	}
	if got := tviewui.IdleColor; got != tcell.NewHexColor(0x64748B) {
		t.Errorf("IdleColor = %v, want 0x64748B", got)
	}
	if got := tviewui.InactiveBorder; got != tcell.NewHexColor(0x334155) {
		t.Errorf("InactiveBorder = %v, want 0x334155", got)
	}
	if got := tviewui.KeyTagColor; got != tcell.NewHexColor(0xA855F7) {
		t.Errorf("KeyTagColor = %v, want 0xA855F7", got)
	}
}

func TestApplyRoundedBorders(t *testing.T) {
	tviewui.ApplyRoundedBorders()
	if tview.Borders.TopLeft != '╭' || tview.Borders.TopRight != '╮' ||
		tview.Borders.BottomLeft != '╰' || tview.Borders.BottomRight != '╯' {
		t.Errorf("corners not rounded: %q %q %q %q",
			tview.Borders.TopLeft, tview.Borders.TopRight,
			tview.Borders.BottomLeft, tview.Borders.BottomRight)
	}
	if tview.Borders.Horizontal != '─' || tview.Borders.Vertical != '│' {
		t.Errorf("edges not light: %q %q", tview.Borders.Horizontal, tview.Borders.Vertical)
	}
	// Focus glyphs must equal normal glyphs — focus is color-coded, not double-line.
	if tview.Borders.HorizontalFocus != tview.Borders.Horizontal ||
		tview.Borders.TopLeftFocus != tview.Borders.TopLeft {
		t.Errorf("focus glyphs must match normal glyphs (no double-line on focus)")
	}
}

func TestGlyphConstants(t *testing.T) {
	if tviewui.GlyphConnected != "●" || tviewui.GlyphIdle != "○" || tviewui.GlyphPointer != "▶" {
		t.Errorf("glyphs wrong: %q %q %q", tviewui.GlyphConnected, tviewui.GlyphIdle, tviewui.GlyphPointer)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tviewui/ -run 'TestTheme|TestApplyRounded' -v`
Expected: FAIL (undefined: tviewui.AccentColor / tviewui.ApplyRoundedBorders)

- [ ] **Step 3: Write minimal implementation**

`internal/tviewui/theme.go`:
```go
package tviewui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// Design tokens (from .local/plan/tui-revamp.md §4.1). Hex values map 1:1 to
// the spec's palette; tcell degrades to 256/16-color on old terminals.
var (
	AccentColor      = tcell.NewHexColor(0x38BDF8) // active borders, headers
	SelectionBG      = tcell.NewHexColor(0x1E293B) // selected row background
	SelectionFG      = tcell.NewHexColor(0x38BDF8) // selected row foreground
	ConnectedColor   = tcell.NewHexColor(0x22C55E) // ● live dot
	IdleColor        = tcell.NewHexColor(0x64748B) // ○ idle dot
	WarningColor     = tcell.NewHexColor(0xF59E0B) // counters, warnings
	InactiveBorder   = tcell.NewHexColor(0x334155) // defocused pane borders
	KeyTagColor      = tcell.NewHexColor(0xA855F7) // footer keybinding brackets
)

// Status glyphs (§4.2). ◌ (connecting) and ▲ (unreachable) are deferred — the
// data model only exposes Live bool.
const (
	GlyphConnected = "●"
	GlyphIdle      = "○"
	GlyphPointer   = "▶"
)

// ApplyRoundedBorders switches the global tview border glyph set to rounded
// Unicode (╭─╮ │ ╰─╯) for both normal AND focus states. Focus is expressed via
// border color (see Task 2), never via line weight — this fixes the
// single-vs-double border mismatch between panes.
func ApplyRoundedBorders() {
	tview.Borders.Horizontal = '─'
	tview.Borders.Vertical = '│'
	tview.Borders.TopLeft = '╭'
	tview.Borders.TopRight = '╮'
	tview.Borders.BottomLeft = '╰'
	tview.Borders.BottomRight = '╯'
	tview.Borders.LeftT = '├'
	tview.Borders.RightT = '┤'
	tview.Borders.TopT = '┬'
	tview.Borders.BottomT = '┴'
	tview.Borders.Cross = '┼'
	tview.Borders.HorizontalFocus = tview.Borders.Horizontal
	tview.Borders.VerticalFocus = tview.Borders.Vertical
	tview.Borders.TopLeftFocus = tview.Borders.TopLeft
	tview.Borders.TopRightFocus = tview.Borders.TopRight
	tview.Borders.BottomLeftFocus = tview.Borders.BottomLeft
	tview.Borders.BottomRightFocus = tview.Borders.BottomRight
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tviewui/ -run 'TestTheme|TestApplyRounded|TestGlyph' -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/tviewui/theme.go internal/tviewui/theme_test.go
git commit -m "feat(tviewui): theme tokens and rounded border glyphs"
```

---

### Task 2: Focus-aware pane border colors

**Why:** Defects #6 (weak focus feedback) and #4 (border inconsistency). The focused pane gets the accent border `#38BDF8`; the defocused pane `#334155`.

**Files:**
- Modify: `internal/tviewui/hostlist.go`, `internal/tviewui/terminal.go`, `internal/tviewui/app.go`
- Test: `internal/tviewui/theme_test.go` (extend)

**Interfaces:**
- Produces on `HostListPane`: `SetFocused(bool)` — sets the list border color (accent/inactive) and list title; `BorderColor() tcell.Color` (via `GetBorderColor`) for tests.
- Produces on `TerminalPane`: `SetFocused(bool)` — sets the active session view / status page border color; `BorderColor() tcell.Color`.
- Consumes: `ApplyRoundedBorders()` from Task 1 (call in `App.New`).

- [ ] **Step 1: Write the failing tests**

Extend `internal/tviewui/theme_test.go`:
```go
func TestHostListPaneFocusBorder(t *testing.T) {
	pane := tviewui.NewHostListPane(sampleHostList())
	pane.Refresh()
	pane.SetFocused(true)
	if got := pane.BorderColor(); got != tviewui.AccentColor {
		t.Errorf("focused border = %v, want accent", got)
	}
	pane.SetFocused(false)
	if got := pane.BorderColor(); got != tviewui.InactiveBorder {
		t.Errorf("unfocused border = %v, want inactive", got)
	}
}

func TestTerminalPaneFocusBorder(t *testing.T) {
	pane := tviewui.NewTerminalPane(nil)
	pane.SetFocused(true)
	if got := pane.BorderColor(); got != tviewui.AccentColor {
		t.Errorf("focused terminal border = %v, want accent", got)
	}
	pane.SetFocused(false)
	if got := pane.BorderColor(); got != tviewui.InactiveBorder {
		t.Errorf("unfocused terminal border = %v, want inactive", got)
	}
}

func TestAppFocusSetsPaneBorders(t *testing.T) {
	app := tviewui.New(sampleHostList(), tviewui.Deps{}, nil)
	app.FocusTerminal()
	if got := app.HostPane().BorderColor(); got != tviewui.InactiveBorder {
		t.Errorf("host pane border while terminal focused = %v, want inactive", got)
	}
	if got := app.TerminalPane().BorderColor(); got != tviewui.AccentColor {
		t.Errorf("terminal pane border while focused = %v, want accent", got)
	}
	app.FocusHostList()
	if got := app.HostPane().BorderColor(); got != tviewui.AccentColor {
		t.Errorf("host pane border while focused = %v, want accent", got)
	}
	if got := app.TerminalPane().BorderColor(); got != tviewui.InactiveBorder {
		t.Errorf("terminal pane border while host focused = %v, want inactive", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tviewui/ -run 'FocusBorder|AppFocusSets' -v`
Expected: FAIL (undefined: pane.BorderColor / SetFocused)

- [ ] **Step 3: Implement**

In `hostlist.go`:
```go
// SetFocused updates the pane's border color and title suffix to reflect
// keyboard focus: accent when focused, inactive otherwise.
func (p *HostListPane) SetFocused(focused bool) {
	style := tcell.StyleDefault.Foreground(InactiveBorder)
	if focused {
		style = tcell.StyleDefault.Foreground(AccentColor)
	}
	p.list.SetBorderStyle(style)
	p.focused = focused
	p.RefreshTitle()
}

// BorderColor returns the current list border color (tests).
func (p *HostListPane) BorderColor() tcell.Color { return p.list.GetBorderColor() }
```
Add field `focused bool` to the struct; extract title building into `RefreshTitle()` so `Refresh()` calls it too. Terminal pane `status` TextView and each session view get the same treatment; `TerminalPane.SetFocused` iterates the active session view (or status page) and applies the border style. In `app.go`, call `ApplyRoundedBorders()` in `New()`, and inside `FocusTerminal`/`FocusHostList` set `pane.SetFocused` before `SetFocus`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tviewui/ -run 'FocusBorder|AppFocusSets' -v`
Expected: PASS. Then `go test ./internal/tviewui/` — all pass.

- [ ] **Step 5: Commit**

```bash
git add internal/tviewui/hostlist.go internal/tviewui/terminal.go internal/tviewui/app.go internal/tviewui/theme_test.go
git commit -m "feat(tviewui): focus-aware accent/inactive pane borders"
```

---

### Task 3: Columnar host rows — status glyph, pointer, truncation, full-line selection

**Why:** Defects #2 (selection bar clipping/gray artifacts) and #3 (mid-string IP truncation). Rows become fixed columns: status glyph → pointer → name (tail-ellipsis) → address (right-aligned, ≥16 cols guaranteed). Foreground-only style tags keep dots colored without breaking the selection background.

**Files:**
- Modify: `internal/tviewui/hostlist.go`
- Test: `internal/tviewui/hostlist_test.go` (extend)

**Interfaces:**
- Produces: `HostListPane.SetRowWidth(int)` (test seam — column math target width; default 44); internal `formatHostRow(e hosts.Entry, width int, selected bool) string`; `SelectedRenderText()` keeps working (returns current row text, now containing `▶` when selected).
- Consumes: `runewidth` (`github.com/mattn/go-runewidth`), glyphs from Task 1.

- [ ] **Step 1: Write the failing tests**

Extend `internal/tviewui/hostlist_test.go`:
```go
func TestHostRowNeverClipsAddress(t *testing.T) {
	// Long alias + long domain: the ADDRESS must never be cut; the NAME takes
	// the truncation hit (defect #3 regression).
	row := tviewui.FormatHostRowForTest(hosts.Entry{
		Alias: "gitlab-infrastructure-prod", HostName: "gitlab.corp.example.net",
		Source: "file",
	}, 34, false)
	if !strings.Contains(row, "gitlab.corp.example.net") {
		t.Errorf("address clipped in %q", row)
	}
	if strings.Contains(row, "gitlab-infrastructure-prod\n") {
		t.Errorf("row unexpectedly long: %q", row)
	}
}

func TestHostRowTailEllipsisOnName(t *testing.T) {
	row := tviewui.FormatHostRowForTest(hosts.Entry{Alias: "gitlab-infrastructure-prod", HostName: "10.0.0.1", Source: "file"}, 34, false)
	// IPv4 needs 15 cols + gap; name must be ellipsized, address fully present.
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tviewui/ -run TestHostRow -v`
Expected: FAIL (undefined: FormatHostRowForTest)

- [ ] **Step 3: Implement column rendering**

`internal/tviewui/hostlist.go` — replace `formatHostLine` with width-aware column layout:

```go
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
```
`Refresh()` then calls `formatHostRow(e, p.rowWidth, idx == p.list.GetCurrentItem())` for each entry, and sets `SetMainTextColor(IdleColor)`/`SetSelectedStyle(bg SelectionBG, fg SelectionFG, bold)` on the list. Update `SelectedRenderText()` to return the row text of the current item (it already does via `GetItemText`). Add `rowWidth` field defaulting to 44 with `SetRowWidth(int)`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tviewui/ -run TestHostRow -v`
Expected: PASS. Then full `go test ./internal/tviewui/` — existing row tests (`TestHostListPaneLiveDot`, `TestHostListPanePasswordBadge`) still pass because ● and pw markers remain in the text.

- [ ] **Step 5: Commit**

```bash
git add internal/tviewui/hostlist.go internal/tviewui/hostlist_test.go
git commit -m "feat(tviewui): columnar host rows with pointer, glyphs, and guaranteed address width"
```

---

### Task 4: Bounded filter header with scope badge + match counter

**Why:** Defect #1 (unbounded floating filter input). The filter becomes a bordered card: title `🔍 Filter [/]`, right-aligned scope badge `Scope: All (7)`, and a live match counter `N matches`.

**Files:**
- Modify: `internal/tviewui/hostlist.go`, `internal/hosts/list.go`
- Test: `internal/tviewui/hostlist_test.go`, `internal/hosts/list_test.go`

**Interfaces:**
- Produces on `hosts.List`: `CountInScope(scope string) int` — launchable entries in scope, **ignoring** the filter (pure logic).
- Produces on `HostListPane`: `MatchCount() int`, `ScopeCount() int`, `ScopeLabel() string`, `FilterTitle() string` (bordered title), `FilterCard() tview.Primitive` (the bordered flex).
- Consumes: glyphs from Task 1.

- [ ] **Step 1: Write the failing tests**

`internal/hosts/list_test.go`:
```go
func TestListCountInScope(t *testing.T) {
	l := NewList([]Entry{
		{Alias: "a", HostName: "1.1.1.1", Source: "file"},
		{Alias: "b", HostName: "2.2.2.2", Source: "file"},
		{Alias: "c", HostName: "3.3.3.3", Source: "vw:personal"},
		{Alias: "d", Source: "file"}, // unlaunchable — excluded
	})
	if got := l.CountInScope(""); got != 3 {
		t.Errorf("CountInScope(all) = %d, want 3", got)
	}
	if got := l.CountInScope("file"); got != 2 {
		t.Errorf("CountInScope(file) = %d, want 2", got)
	}
	if got := l.CountInScope("vw:personal"); got != 1 {
		t.Errorf("CountInScope(vw:personal) = %d, want 1", got)
	}
	l.SetFilter("a")
	if got := l.CountInScope(""); got != 3 {
		t.Errorf("CountInScope must ignore filter, got %d", got)
	}
}
```

`internal/tviewui/hostlist_test.go`:
```go
func TestFilterCardBoundedAndCounted(t *testing.T) {
	pane := tviewui.NewHostListPane(sampleHostList())
	pane.SetFilter("prod")
	pane.Refresh()
	if pane.MatchCount() != 1 {
		t.Errorf("MatchCount = %d, want 1", pane.MatchCount())
	}
	if pane.ScopeCount() != 3 {
		t.Errorf("ScopeCount = %d, want 3", pane.ScopeCount())
	}
	if !strings.Contains(pane.FilterTitle(), "Filter") {
		t.Errorf("filter title missing label: %q", pane.FilterTitle())
	}
}

func TestFilterCardScopeBadge(t *testing.T) {
	pane := tviewui.NewHostListPane(sampleHostList())
	pane.SetScope("file")
	pane.Refresh()
	if !strings.Contains(pane.ScopeLabel(), "~/.ssh/config") {
		t.Errorf("ScopeLabel = %q, want friendly file label", pane.ScopeLabel())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/hosts/ ./internal/tviewui/ -run 'CountInScope|FilterCard' -v`
Expected: FAIL (undefined methods)

- [ ] **Step 3: Implement**

In `internal/hosts/list.go`:
```go
// CountInScope returns the number of launchable entries in scope, ignoring the
// active filter (used for the scope badge counter).
func (l *List) CountInScope(scope string) int {
	n := 0
	for _, e := range l.entries {
		if e.HostName == "" {
			continue
		}
		if scope != "" && e.Source != scope {
			continue
		}
		n++
	}
	return n
}
```

In `hostlist.go`: replace the bare `filter` InputField with a bordered card:
```go
p.filter.SetBorder(true).SetTitle(" 🔍 Filter [/] ").SetTitleAlign(tview.AlignLeft)
p.filter.SetLabel("> ").SetFieldWidth(0)
p.scopeText = tview.NewTextView().SetTextAlign(tview.AlignRight).SetDynamicColors(true)
p.flex = tview.NewFlex().SetDirection(tview.FlexRow).
	AddItem(p.filterCard(), 3, 0, true).
	AddItem(p.list, 0, 1, false)
```
`filterCard()` returns a FlexRow: `[filter (1,0,true), scopeText (fixed 20)]`; `Refresh()` sets `scopeText` to `Scope: <label> (<count>)` and updates `MatchCount()` from `len(p.entries)` / `CountInScope`. `ScopeLabel()` reuses `scopeLabel()`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/hosts/ ./internal/tviewui/ -run 'CountInScope|FilterCard' -v`
Expected: PASS; then `go test ./...` — verify `TestHostListPaneSetSyncStatus` etc. still pass (keep sync status in the list title).

- [ ] **Step 5: Commit**

```bash
git add internal/hosts/list.go internal/hosts/list_test.go internal/tviewui/hostlist.go internal/tviewui/hostlist_test.go
git commit -m "feat(tviewui): bounded filter card with scope badge and match counter"
```

---

### Task 5: Scope switcher modal (Ctrl+B)

**Why:** Phase 2 — the scope cycle moves from Tab into a discoverable overlay modal with per-scope counts and a checkmark.

**Files:**
- Create: `internal/tviewui/scopemodal.go`
- Test: `internal/tviewui/scopemodal_test.go`
- Modify: `internal/tviewui/app.go`, `internal/tviewui/hostlist.go`

**Interfaces:**
- Produces: `ScopeModal` with `NewScopeModal(scopes []string, counts map[string]int, current string) *ScopeModal`, `Primitive() tview.Primitive`, `SetOnSelect(func(scope string))`, `SetOnCancel(func())`, `TriggerSelect()`, `TriggerCancel()`, `TriggerKey(*tcell.EventKey)`, `Current() string` (tests).
- Consumes: `hosts.List.Scopes()`, `CountInScope()` from Task 4.

- [ ] **Step 1: Write the failing tests**

`internal/tviewui/scopemodal_test.go`:
```go
package tviewui_test

import (
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/ac-kurniawan/wardenssh/internal/tviewui"
)

func TestScopeModalListsScopesWithCounts(t *testing.T) {
	m := tviewui.NewScopeModal([]string{"", "vw:work", "file"}, map[string]int{"": 7, "vw:work": 3, "file": 2}, "")
	text := m.Current()
	if text == "" {
		t.Fatal("expected a current scope row")
	}
	if !strings.Contains(text, "7") || !strings.Contains(text, "All") {
		t.Errorf("all-scope row should show count 7 and label All: %q", text)
	}
}

func TestScopeModalSelectCallsBack(t *testing.T) {
	m := tviewui.NewScopeModal([]string{"", "file"}, map[string]int{"": 7, "file": 2}, "")
	var got string
	m.SetOnSelect(func(s string) { got = s })
	// Move down to "file", select it.
	m.TriggerKey(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone))
	m.TriggerSelect()
	if got != "file" {
		t.Errorf("selected = %q, want file", got)
	}
}

func TestScopeModalCancel(t *testing.T) {
	m := tviewui.NewScopeModal([]string{"", "file"}, map[string]int{"": 7, "file": 2}, "")
	cancelled := false
	m.SetOnCancel(func() { cancelled = true })
	m.TriggerCancel()
	if !cancelled {
		t.Error("expected cancel callback")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tviewui/ -run TestScopeModal -v`
Expected: FAIL (undefined: NewScopeModal)

- [ ] **Step 3: Implement**

`scopemodal.go` — a centered bordered List inside a modal flex:
```go
// ScopeModal is the Ctrl+B overlay: pick a host scope/group. Rows show a
// checkmark for the current scope, a friendly label, and the host count.
type ScopeModal struct {
	modal    *tview.Flex
	list     *tview.List
	scopes   []string
	onSelect func(string)
	onCancel func()
}

func NewScopeModal(scopes []string, counts map[string]int, current string) *ScopeModal {
	m := &ScopeModal{scopes: scopes}
	m.list = tview.NewList().ShowSecondaryText(false).SetBorder(true).
		SetTitle(" Select Host Scope / Group ").SetTitleAlign(tview.AlignLeft)
	for _, s := range scopes {
		label, cnt := scopeOption(s, counts)
		mark := "[ ]"
		if s == current {
			mark = "[*]"
		}
		m.list.AddItem(fmt.Sprintf("%s %-18s (%d)", mark, label, cnt), "", 0, nil)
	}
	m.list.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEnter {
			m.triggerSelect()
			return nil
		}
		if event.Key() == tcell.KeyEscape {
			m.triggerCancel()
			return nil
		}
		return event
	})
	box := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(nil, 0, 1, false).AddItem(m.list, 0, 0, true).AddItem(nil, 0, 1, false)
	m.modal = tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(nil, 0, 1, false).AddItem(box, 40, 0, true).AddItem(nil, 0, 1, false)
	return m
}
```
`scopeOption(s, counts)` reuses `scopeLabel` semantics ("All Hosts" for "", "~/.ssh/config" for file). `Current()` returns the focused row text. Wire in `app.go`: `handleGlobalKeys` — Ctrl+B opens the scope modal (overlay page "scope"), on select → `hostList.SetScope(s)` + `hostPane.Refresh()` + close; keep Ctrl+B out of terminal forwarding.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tviewui/ -run TestScopeModal -v`
Expected: PASS. Note: `pane_test.go::TestAppCtrlBTogglesBetweenPanes` will now FAIL — updated in Task 6.

- [ ] **Step 5: Commit**

```bash
git add internal/tviewui/scopemodal.go internal/tviewui/scopemodal_test.go internal/tviewui/app.go internal/tviewui/hostlist.go
git commit -m "feat(tviewui): scope switcher modal (Ctrl+B)"
```

---

### Task 6: Keymap overhaul

**Why:** §6 keymap. Tab no longer cycles scope → focuses terminal; Ctrl+B → scope modal; Ctrl+\ → sidebar; / → filter; ? → help; Ctrl+Q → quit; Ctrl+D → disconnect; Delete key → delete; j/k → move selection.

**Files:**
- Modify: `internal/tviewui/app.go`, `internal/tviewui/hostlist.go`
- Test: `internal/tviewui/pane_test.go`, `internal/tviewui/hostlist_test.go`, `internal/tviewui/app_test.go`

- [ ] **Step 1: Update tests to the new keymap (red)**

`pane_test.go` — replace `TestAppCtrlBTogglesBetweenPanes`:
```go
func TestAppTabFocusesTerminalAndCtrlBackslashReturns(t *testing.T) {
	hl := sampleHostList()
	app := tviewui.New(hl, tviewui.Deps{}, nil)

	// No session: Tab stays on host list (nothing to focus).
	ev := app.HandleGlobalKey(tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone))
	if app.FocusedPane() != "host" {
		t.Errorf("Tab with no session: FocusedPane = %q, want host", app.FocusedPane())
	}

	// Session running: Tab moves into the terminal, Ctrl+\ returns.
	app.TerminalPane().SetRunningForTest(true)
	ev = app.HandleGlobalKey(tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone))
	if app.FocusedPane() != "terminal" {
		t.Errorf("Tab: FocusedPane = %q, want terminal", app.FocusedPane())
	}
	_ = ev
	ev = app.HandleGlobalKey(tcell.NewEventKey(tcell.KeyCtrlBackslash, 0, tcell.ModNone))
	if app.FocusedPane() != "host" {
		t.Errorf("Ctrl+\\: FocusedPane = %q, want host", app.FocusedPane())
	}
	_ = ev
}

func TestAppCtrlBInHostOpensScopeModal(t *testing.T) {
	hl := sampleHostList()
	app := tviewui.New(hl, tviewui.Deps{}, nil)
	if app.InScopeModal() {
		t.Fatal("precondition: not in scope modal")
	}
	app.HandleGlobalKey(tcell.NewEventKey(tcell.KeyCtrlB, 0, tcell.ModNone))
	if !app.InScopeModal() {
		t.Fatal("Ctrl+B must open the scope modal")
	}
	app.CancelScopeModal()
	if app.InScopeModal() {
		t.Fatal("expected scope modal dismissed")
	}
}
```

`app_test.go` — add:
```go
func TestAppCtrlQQuits(t *testing.T) {
	hl := sampleHostList()
	app := tviewui.New(hl, tviewui.Deps{}, nil)
	app.HandleGlobalKey(tcell.NewEventKey(tcell.KeyCtrlQ, 0, tcell.ModNone))
	if !app.InQuitModal() {
		t.Fatal("expected quit modal after Ctrl+Q")
	}
}

func TestAppSlashFocusesFilter(t *testing.T) {
	hl := sampleHostList()
	app := tviewui.New(hl, tviewui.Deps{}, nil)
	if app.HostPane().FilterFocused() {
		t.Fatal("precondition: filter not focused")
	}
	app.HandleGlobalKey(tcell.NewEventKey(tcell.KeyRune, '/', tcell.ModNone))
	if !app.HostPane().FilterFocused() {
		t.Error("expected '/' to focus the filter input")
	}
}
```

`hostlist_test.go` — replace `TestHostListPaneTabCyclesScope`:
```go
func TestHostListPaneJKMovesSelection(t *testing.T) {
	pane := tviewui.NewHostListPane(sampleHostList())
	pane.Refresh()
	e, _ := pane.SelectedEntry()
	if e.Alias != "prod-db-01" {
		t.Fatalf("initial = %q", e.Alias)
	}
	pane.HandleListKey(tcell.NewEventKey(tcell.KeyRune, 'j', tcell.ModNone))
	e, _ = pane.SelectedEntry()
	if e.Alias != "web-02" {
		t.Errorf("after j = %q, want web-02", e.Alias)
	}
	pane.HandleListKey(tcell.NewEventKey(tcell.KeyRune, 'k', tcell.ModNone))
	e, _ = pane.SelectedEntry()
	if e.Alias != "prod-db-01" {
		t.Errorf("after k = %q, want prod-db-01", e.Alias)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/tviewui/ -run 'TabFocuses|CtrlBInHost|CtrlQ|SlashFocuses|JKMoves' -v`
Expected: FAIL (undefined FilterFocused/HandleListKey/InScopeModal; Tab still cycles scope; Ctrl+B still toggles)

- [ ] **Step 3: Implement**

`app.go handleGlobalKeys`:
- Host focus: `Tab` → `FocusTerminal()` when a session runs (else no-op); `KeyCtrlBackslash` → `FocusHostList()` (only meaningful from terminal); Ctrl+B → `showScopeModal()`; `/` → `hostPane.FocusFilter(app)`; `?` → help modal (Task 7, stub returns false for now); Ctrl+Q → `RequestQuit()` (in addition to q/Esc/Ctrl+C); Ctrl+D in host mode → if selected entry is live, `showDisconnectModal(entry)` (else no-op).
- Terminal focus: Tab is forwarded to the terminal (existing default); `KeyCtrlBackslash` → `FocusHostList()`; Ctrl+D → disconnect the active session (existing disconnect modal path via `ActiveEntry`).

`hostlist.go`:
- Remove `Tab` → `hostList.Tab()` from both input captures (scope cycling now via modal).
- List capture: add `'j'`→down, `'k'`→up; `KeyDelete` only → delete (drop Ctrl+D delete); expose `HandleListKey(*tcell.EventKey) *tcell.EventKey` and `FilterFocused() bool` for tests.
- Keep `TabNext()` for the scope modal / tests.

- [ ] **Step 4: Run full suite**

Run: `go test ./internal/tviewui/`
Expected: PASS (all updated keymap tests green). Fix any stragglers (`TestAppEscInTerminalForwardsOtherKeys` still passes — Ctrl+\ not in its list).

- [ ] **Step 5: Commit**

```bash
git add internal/tviewui/app.go internal/tviewui/hostlist.go internal/tviewui/pane_test.go internal/tviewui/hostlist_test.go internal/tviewui/app_test.go
git commit -m "feat(tviewui): revamped keymap (Tab/Ctrl+\\/Ctrl+B// /Ctrl+Q/Ctrl+D)"
```

---

### Task 7: Help sheet (`?`)

**Why:** §6 `?` opens an interactive help sheet; footer references it.

**Files:**
- Create: `internal/tviewui/helpmodal.go`
- Test: `internal/tviewui/helpmodal_test.go`
- Modify: `internal/tviewui/app.go`

**Interfaces:**
- Produces: `HelpModal` with `NewHelpModal(mode string) *HelpModal`, `Primitive()`, `SetOnClose(func())`, `TriggerClose()`, `Text() string` (tests).
- Consumes: Task 6 keymap (text content).

- [ ] **Step 1: Write failing tests**

```go
func TestHelpModalHostContent(t *testing.T) {
	m := tviewui.NewHelpModal("host")
	text := m.Text()
	for _, want := range []string{"Tab", "Ctrl+B", "/", "?", "Ctrl+Q", "Enter", "j", "k"} {
		if !strings.Contains(text, want) {
			t.Errorf("help missing %q:\n%s", want, text)
		}
	}
}

func TestHelpModalClose(t *testing.T) {
	m := tviewui.NewHelpModal("host")
	closed := false
	m.SetOnClose(func() { closed = true })
	m.TriggerClose()
	if !closed {
		t.Error("expected close callback")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Expected: FAIL (undefined NewHelpModal)

- [ ] **Step 3: Implement**

`helpmodal.go` — a bordered TextView listing context keybindings, Esc/Enter closes. Wire `?` in `app.handleGlobalKeys` (host mode) → overlay page "help"; `SetOnClose` removes page and refocuses.

- [ ] **Step 4: Run to verify pass + full suite**

`go test ./internal/tviewui/` — PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tviewui/helpmodal.go internal/tviewui/helpmodal_test.go internal/tviewui/app.go
git commit -m "feat(tviewui): interactive help sheet on '?'"
```

---

### Task 8: Footer redesign with key tags

**Why:** Defect #7 (low-density footer). New two-row footer: context hints + global keys, key names in violet `[#A855F7]`.

**Files:**
- Modify: `internal/tviewui/footer.go`, `internal/tviewui/app.go`
- Test: `internal/tviewui/footer_test.go`, `internal/tviewui/app_test.go`

- [ ] **Step 1: Update tests (red)**

`footer_test.go`:
```go
func TestFooterHostModeHints(t *testing.T) {
	f := tviewui.NewFooter()
	text := f.Text()
	for _, want := range []string{"Select", "Connect", "Terminal", "Filter", "Scopes", "Quit"} {
		if !strings.Contains(text, want) {
			t.Errorf("host footer %q missing %q", text, want)
		}
	}
}

func TestFooterUsesKeyTagColor(t *testing.T) {
	f := tviewui.NewFooter()
	if !strings.Contains(f.Text(), "#A855F7") {
		t.Errorf("footer must use violet key tags: %q", f.Text())
	}
}

func TestFooterSwitchesToTerminalMode(t *testing.T) {
	f := tviewui.NewFooter()
	f.SetMode("terminal")
	text := f.Text()
	for _, want := range []string{"Ctrl+\\", "Ctrl+D", "Copy"} {
		if !strings.Contains(text, want) {
			t.Errorf("terminal footer %q missing %q", text, want)
		}
	}
}
```

- [ ] **Step 2: Run to verify failure**

Expected: FAIL (footer text unchanged)

- [ ] **Step 3: Implement**

`footer.go` — two-line TextView (height 2 in app layout):
- Host mode line 1 (context): `[#A855F7][↑/↓][-] Select  [#A855F7][↵][-] Connect  [#A855F7][⇥][-] Terminal`; line 2 (global): `[#A855F7][?][-] Help  [#A855F7][/][-] Filter  [#A855F7][Ctrl+B][-] Scopes  [#A855F7][Ctrl+Q][-] Quit`.
- Terminal mode line 1: `[#A855F7][Ctrl+\][-] Sidebar  [#A855F7][Ctrl+Shift+C][-] Copy  [#A855F7][Ctrl+D][-] Disconnect`; line 2 unchanged global line.
`app.go`: footer height 1 → 2 in the root flex.

- [ ] **Step 4: Run to verify pass + full suite**

`go test ./internal/tviewui/` — PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tviewui/footer.go internal/tviewui/footer_test.go internal/tviewui/app.go internal/tviewui/app_test.go
git commit -m "feat(tviewui): two-row footer with violet keybinding tags"
```

---

### Task 9: Terminal pane title polish + session uptime

**Why:** §3.2 mockup — the terminal title carries `💻 alias (host) [CONNECTED]` / `[ACTIVE SESSION]`, and Phase 3 telemetry adds uptime (ping deferred per Global Constraints).

**Files:**
- Modify: `internal/tviewui/terminal.go`, `internal/tviewui/terminal_view.go`, `internal/tviewui/app.go`
- Test: `internal/tviewui/terminal_test.go`

**Interfaces:**
- Produces on `TerminalPane`: `SetSessionTitleState(focused bool)` — updates the active view title with `[ACTIVE SESSION]` vs `[CONNECTED]`; `ActiveTitle() string` (tests); internal `formatSessionTitle(alias, host, state string, up time.Duration) string`.
- Consumes: `runewidth` for truncation.

- [ ] **Step 1: Write failing tests**

```go
func TestFormatSessionTitle(t *testing.T) {
	if got := tviewui.FormatSessionTitleForTest("tencent1", "43.129.40.8", "CONNECTED", 0); got != "💻 tencent1 (43.129.40.8) [CONNECTED] · Up: 0s" {
		t.Errorf("title = %q", got)
	}
	if got := tviewui.FormatSessionTitleForTest("tencent1", "43.129.40.8", "ACTIVE SESSION", 90*time.Second); !strings.Contains(got, "[ACTIVE SESSION]") || !strings.Contains(got, "1m30s") {
		t.Errorf("title = %q", got)
	}
}

func TestTerminalPaneTitleState(t *testing.T) {
	pane := tviewui.NewTerminalPane(nil)
	defer pane.Close()
	key := tviewui.SessionKey("host-a", "file")
	pane.SetSessionForTest(key, "host-a", "file")
	pane.SetSessionTitleState(true)
	if !strings.Contains(pane.ActiveTitle(), "ACTIVE SESSION") {
		t.Errorf("focused title = %q, want ACTIVE SESSION", pane.ActiveTitle())
	}
	pane.SetSessionTitleState(false)
	if !strings.Contains(pane.ActiveTitle(), "CONNECTED") {
		t.Errorf("unfocused title = %q, want CONNECTED", pane.ActiveTitle())
	}
}
```

- [ ] **Step 2: Run to verify failure**

Expected: FAIL (undefined FormatSessionTitleForTest / SetSessionTitleState / ActiveTitle)

- [ ] **Step 3: Implement**

`terminal.go`: `terminalSession` gains `host string`, `started time.Time`, `viewTitle string`. `StartSSHFromCmd` records `entry.HostName` and `time.Now()`. `formatSessionTitle` composes the mockup string (host truncated with `…` beyond 30 cols). `SetSessionTitleState(focused)` writes the title on the active view and stores it in `ActiveTitle()`. App calls it inside `FocusTerminal`/`FocusHostList` (after Task 2's `SetFocused`). `newTerminalView` keeps its border (rounded via global glyphs); session views carry the same border style handling as the pane.

- [ ] **Step 4: Run full suite**

`go test ./internal/tviewui/` — PASS (existing terminal tests assert `ActiveEntry`, unaffected; `terminalView` title tests in `terminal_copy_test.go` build their own views).

- [ ] **Step 5: Commit**

```bash
git add internal/tviewui/terminal.go internal/tviewui/terminal_test.go internal/tviewui/app.go
git commit -m "feat(tviewui): terminal title with alias/host/state and session uptime"
```

---

## Self-Review

**Spec coverage (§ roadmap):**
- Phase 1 rounded borders → Task 1; flex widths → existing 1:2 proportions (documented); full-width selection → Task 3.
- Phase 2 bounded filter + match counters → Task 4; Ctrl+B scope modal → Task 5.
- Phase 3 PTY isolation → already satisfied by tvxterm (no task); session telemetry → Task 9 (uptime; ping deferred).
- §4 design tokens → Task 1; §4.2 glyphs → Tasks 1/3; §5.2 truncation hierarchy → Task 3; §6 keymap → Tasks 5–8.

**Placeholder scan:** no TBD/TODO; every task has concrete tests + code.

**Type consistency:** `SetFocused` (T2/T9) vs `SetSessionTitleState` (T9) are distinct concerns; `BorderColor()` used consistently; `CountInScope` defined in Task 4 and consumed by Tasks 4/5; `FormatHostRowForTest`/`FormatSessionTitleForTest` are the only exported test seams.

**Deviations documented:** ping telemetry deferred; ◌/▲ glyphs deferred; responsive min/max column clamping deferred (proportional 1:2 instead); two-row footer instead of per-pane footers.