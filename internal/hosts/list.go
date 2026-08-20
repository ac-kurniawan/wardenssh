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
	Alias        string // display label (vault item name or ssh_config alias)
	HostName     string
	User         string
	Port         string
	ProxyJump    string
	Source       string // "file" or "vw:<vault-name>"
	Live         bool   // green-dot: a session to this host is currently open
	IdentityFile string // path to private key file (file-sourced entries only)
	AuthKind     string // "key" (default) | "password" — how connect authenticates
	Wildcard     bool   // true for wildcard blocks (e.g. Host * or *.domain)
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
// Entries with an empty HostName (unlaunchable) are always hidden.
func (l *List) Visible() []Entry {
	scope := l.Scope()
	var out []Entry
	for _, e := range l.entries {
		if e.HostName == "" {
			continue // unlaunchable — hide
		}
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

// All returns every entry regardless of the current scope/filter — used by the
// TUI to detect any live session across all scopes for the quit-confirmation
// modal (Q31/C) and by kill-all to clear all live flags.
func (l *List) All() []Entry {
	out := make([]Entry, len(l.entries))
	copy(out, l.entries)
	return out
}

// Merge appends new entries (typically from a vault source that became
// available after setup) and recomputes the scope cycle. Existing live
// flags are preserved. The current filter is lazy — Visible() will pick
// up the new entries automatically on the next render.
func (l *List) Merge(newEntries []Entry) {
	l.entries = append(l.entries, newEntries...)
	l.scopes = l.deriveScopes()
}

// ReplaceVaultEntries replaces all entries belonging to the given source
// with newEntries, preserving existing Live session flags for matching aliases.
func (l *List) ReplaceVaultEntries(source string, newEntries []Entry) {
	live := make(map[string]bool)
	for _, e := range l.entries {
		if e.Source == source && e.Live {
			live[e.Alias] = true
		}
	}

	var updated []Entry
	for _, e := range l.entries {
		if e.Source != source {
			updated = append(updated, e)
		}
	}

	for _, ne := range newEntries {
		if live[ne.Alias] {
			ne.Live = true
		}
		updated = append(updated, ne)
	}

	l.entries = updated
	l.scopes = l.deriveScopes()
	l.SetScope(l.Scope())
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

// deriveScopes produces the ordered scope cycle: "" (all) first, then each
// vault source in stable order, then "file" last. A source is a vault scope
// if its label is anything other than "file" (the runtime label is the vault
// config name, e.g. "vw" — not "vw:<name>"). Sources whose entries all have
// an empty HostName are skipped (nothing launchable to show in that scope).
func (l *List) deriveScopes() []string {
	var vaults []string
	seenVault := map[string]bool{}
	hasFile := false
	for _, e := range l.entries {
		if e.HostName == "" {
			continue // not launchable; never contributes a scope
		}
		switch {
		case e.Source == "file":
			hasFile = true
		case e.Source != "":
			if !seenVault[e.Source] {
				seenVault[e.Source] = true
				vaults = append(vaults, e.Source)
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

// Remove deletes an entry matching alias and source from the list and updates available scopes.
func (l *List) Remove(alias, source string) {
	var next []Entry
	for _, e := range l.entries {
		if e.Alias == alias && e.Source == source {
			continue
		}
		next = append(next, e)
	}
	l.entries = next
	l.scopes = l.deriveScopes()
	if l.scopeIdx >= len(l.scopes) {
		l.scopeIdx = 0
	}
}

// Replace updates an existing entry matching oldAlias and oldSource with newEntry in-place,
// preserving the Live state if set. If the old entry is not found, newEntry is appended.
func (l *List) Replace(oldAlias, oldSource string, newEntry Entry) {
	for i, e := range l.entries {
		if e.Alias == oldAlias && e.Source == oldSource {
			if e.Live {
				newEntry.Live = true
			}
			l.entries[i] = newEntry
			l.scopes = l.deriveScopes()
			if l.scopeIdx >= len(l.scopes) {
				l.scopeIdx = 0
			}
			return
		}
	}
	l.Merge([]Entry{newEntry})
}

