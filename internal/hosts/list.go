// Package hosts holds the unified, source-merged host list that the launcher
// TUI renders (Q10/C dual-source, Q11/C no-dedup + source badges, Q18/iii
// live-session green dots, Q29/B fuzzy filter + source-scope cycling).
//
// It is pure logic (no Bubble Tea) so it can be unit-tested directly; the TUI
// model in internal/tui wraps this and renders it.
package hosts

import (
	"sort"
	"strings"
)

// Entry is a single launchable host, regardless of source.
type Entry struct {
	Alias     string // display label (vault item name or ssh_config alias)
	HostName  string
	User      string
	Port      string
	ProxyJump string
	Source    string // "file" or "vw:<vault-name>"
	Live      bool   // green-dot: a session to this host is currently open
}

// List is the merged, filterable, scope-cyclable host list.
type List struct {
	entries  []Entry
	scopes   []string // ordered: "" (all) first, then vaults in stable order, then "file"
	scopeIdx int
	filter   string
}

// NewList builds a list from the given entries and derives the scope cycle
// from their distinct sources. Scope order: "" (all), then each vault source
// in stable first-seen order, then "file" last — matching Q29/B's
// all -> per-vault -> file-only -> all cycle.
func NewList(entries []Entry) *List {
	l := &List{entries: append([]Entry(nil), entries...)}
	l.scopes = l.deriveScopes()
	return l
}

// Scopes returns the ordered scope cycle ("" represents "all sources").
func (l *List) Scopes() []string { return l.scopes }

// Scope returns the current scope label ("" = all).
func (l *List) Scope() string {
	if l.scopeIdx < 0 || l.scopeIdx >= len(l.scopes) {
		return ""
	}
	return l.scopes[l.scopeIdx]
}

// SetScope sets the current scope; an unknown/empty scope falls back to "".
func (l *List) SetScope(s string) {
	for i, sc := range l.scopes {
		if sc == s {
			l.scopeIdx = i
			return
		}
	}
	l.scopeIdx = 0
}

// Tab advances the scope to the next in the cycle, wrapping back to "" (all).
func (l *List) Tab() {
	if len(l.scopes) == 0 {
		return
	}
	l.scopeIdx = (l.scopeIdx + 1) % len(l.scopes)
}

// SetFilter narrows the list to aliases matching the (fzf-style subsequence,
// case-insensitive) filter. An empty filter shows all entries in scope.
func (l *List) SetFilter(f string) { l.filter = f }

// Visible returns the entries in the current scope matching the current filter.
func (l *List) Visible() []Entry {
	scope := l.Scope()
	var out []Entry
	for _, e := range l.entries {
		if scope != "" && e.Source != scope {
			continue
		}
		if l.filter != "" && !fuzzyMatch(e.Alias, l.filter) {
			continue
		}
		out = append(out, e)
	}
	return out
}

// All returns every entry regardless of the current scope/filter — used by the
// TUI to detect any live session across all scopes for the quit-confirmation
// modal (Q31/C) and by kill-all to clear all live flags.
func (l *List) All() []Entry {
	out := make([]Entry, len(l.entries))
	copy(out, l.entries)
	return out
}

// MarkLive flags the entry (matched by alias + source) as having an open
// session (green dot). No-op if not found.
func (l *List) MarkLive(alias, source string) {
	l.setLive(alias, source, true)
}

// MarkDead clears the live flag for the entry (matched by alias + source).
// No-op if not found.
func (l *List) MarkDead(alias, source string) {
	l.setLive(alias, source, false)
}

func (l *List) setLive(alias, source string, live bool) {
	for i := range l.entries {
		if l.entries[i].Alias == alias && l.entries[i].Source == source {
			l.entries[i].Live = live
			return
		}
	}
}

// deriveScopes produces the ordered scope cycle.
func (l *List) deriveScopes() []string {
	var vaults []string
	seenVault := map[string]bool{}
	hasFile := false
	for _, e := range l.entries {
		switch {
		case e.Source == "file":
			hasFile = true
		case strings.HasPrefix(e.Source, "vw:"):
			name := e.Source
			if !seenVault[name] {
				seenVault[name] = true
				vaults = append(vaults, name)
			}
		}
	}
	sort.Strings(vaults)
	scopes := []string{""}
	scopes = append(scopes, vaults...)
	if hasFile {
		scopes = append(scopes, "file")
	}
	return scopes
}

// fuzzyMatch reports whether the chars of needle appear in haystack in order,
// case-insensitively (fzf-style subsequence match).
func fuzzyMatch(haystack, needle string) bool {
	hay := strings.ToLower(haystack)
	ndl := strings.ToLower(needle)
	i := 0
	for j := 0; j < len(ndl); j++ {
		c := ndl[j]
		found := false
		for i < len(hay) {
			if hay[i] == c {
				i++
				found = true
				break
			}
			i++
		}
		if !found {
			return false
		}
	}
	return true
}