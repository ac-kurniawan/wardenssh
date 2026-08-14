package hosts_test

import (
	"testing"

	"github.com/ac-kurniawan/wardenssh/internal/hosts"
)

// sampleEntries returns a mixed-source set used across assertions:
//   file:       prod-db-01, web-02
//   vw:personal: gitlab-runner, ci-box
//   vw:work:     bastion-prod
func sampleEntries() []hosts.Entry {
	return []hosts.Entry{
		{Alias: "prod-db-01", HostName: "10.0.0.5", Source: "file"},
		{Alias: "web-02", HostName: "web-02.internal", Source: "file"},
		{Alias: "gitlab-runner", HostName: "10.1.0.9", Source: "vw:personal"},
		{Alias: "ci-box", HostName: "10.1.0.10", Source: "vw:personal"},
		{Alias: "bastion-prod", HostName: "b.internal", Source: "vw:work"},
	}
}

// TestScopesBuiltFromSources: NewList derives an ordered scope cycle
// ["", "vw:personal", "vw:work", "file"] from the entries (all first, then
// vaults in stable order, then file) — matching Q29/B's
// all -> per-vault -> file-only -> all cycle.
func TestScopesBuiltFromSources(t *testing.T) {
	l := hosts.NewList(sampleEntries())
	want := []string{"", "vw:personal", "vw:work", "file"}
	if got := l.Scopes(); !eqStr(got, want) {
		t.Errorf("Scopes = %v, want %v", got, want)
	}
}

// TestScopesIncludeRuntimeVaultLabel: the runtime vault source label is the
// vault's config name (e.g. "vw"), NOT "vw:<name>". deriveScopes must treat
// any non-file source as a vault scope so the cycle includes vaults.
func TestScopesIncludeRuntimeVaultLabel(t *testing.T) {
	l := hosts.NewList([]hosts.Entry{
		{Alias: "prod-db-01", HostName: "10.0.0.5", Source: "file"},
		{Alias: "gitlab", HostName: "10.1.0.9", Source: "vw"},
	})
	got := l.Scopes()
	if !contains(got, "vw") {
		t.Errorf("Scopes = %v, want it to include the vault label 'vw'", got)
	}
	if !contains(got, "file") {
		t.Errorf("Scopes = %v, want it to include 'file'", got)
	}
}

// TestVisibleSkipsEmptyHost: entries with an empty HostName (unlaunchable)
// must be hidden from the visible list.
func TestVisibleSkipsEmptyHost(t *testing.T) {
	l := hosts.NewList([]hosts.Entry{
		{Alias: "good", HostName: "10.0.0.5", Source: "file"},
		{Alias: "bad", HostName: "", Source: "file"},
	})
	vis := l.Visible()
	if len(vis) != 1 || vis[0].Alias != "good" {
		t.Errorf("Visible = %+v, want only [good] (empty-host hidden)", vis)
	}
}

// TestScopesSkipEmptyVault: a vault source whose entries all have an empty
// HostName must not appear as a scope (nothing to show in it).
func TestScopesSkipEmptyVault(t *testing.T) {
	l := hosts.NewList([]hosts.Entry{
		{Alias: "good", HostName: "10.0.0.5", Source: "file"},
		{Alias: "empty", HostName: "", Source: "vw:empty"},
	})
	if contains(l.Scopes(), "vw:empty") {
		t.Errorf("Scopes = %v, want empty vault scope 'vw:empty' skipped", l.Scopes())
	}
	if !contains(l.Scopes(), "file") {
		t.Errorf("Scopes = %v, want 'file' scope present", l.Scopes())
	}
}

// TestVisibleAllScope: scope "" (all) returns every entry.
func TestVisibleAllScope(t *testing.T) {
	l := hosts.NewList(sampleEntries())
	if got := len(l.Visible()); got != 5 {
		t.Errorf("all scope: got %d, want 5", got)
	}
}

// TestVisiblePerVaultScope: scope "vw:personal" returns only that vault's entries.
func TestVisiblePerVaultScope(t *testing.T) {
	l := hosts.NewList(sampleEntries())
	l.SetScope("vw:personal")
	if got := l.Visible(); len(got) != 2 {
		t.Errorf("vw:personal: got %d entries, want 2", len(got))
	} else if got[0].Source != "vw:personal" {
		t.Errorf("vw:personal first entry source = %q", got[0].Source)
	}
}

// TestVisibleFileScope: scope "file" returns only file-sourced entries.
func TestVisibleFileScope(t *testing.T) {
	l := hosts.NewList(sampleEntries())
	l.SetScope("file")
	if got := l.Visible(); len(got) != 2 {
		t.Errorf("file: got %d entries, want 2", len(got))
	}
}

// TestTabCyclesScopesInOrder: Tab advances through scopes in documented order
// and wraps back to "" (all) after "file".
func TestTabCyclesScopesInOrder(t *testing.T) {
	l := hosts.NewList(sampleEntries())
	want := []string{"", "vw:personal", "vw:work", "file", ""}
	for _, w := range want {
		if got := l.Scope(); got != w {
			t.Errorf("Scope() = %q, want %q", got, w)
		}
		l.Tab()
	}
}

// TestFuzzyFilterSubsequence: a filter matches characters as an in-order
// subsequence (fzf-style) of the alias, case-insensitive.
func TestFuzzyFilterSubsequence(t *testing.T) {
	cases := map[string]int{
		"":     5, // empty filter shows all in scope
		"pdb":  1, // prod-db-01 (p,d,b subsequence); bastion-prod lacks 'b' after 'd'
		"prod": 2, // prod-db-01 AND bastion-prod (both contain p,r,o,d in order)
		"web":  1, // web-02
		"CI":   1, // ci-box (case-insensitive)
		"zzz":  0, // no match
	}
	for filter, want := range cases {
		l := hosts.NewList(sampleEntries())
		l.SetFilter(filter)
		if got := len(l.Visible()); got != want {
			t.Errorf("filter %q: got %d, want %d", filter, got, want)
		}
	}
}

// TestFilterComposesWithScope: filter applies within the current scope, not
// across all sources.
func TestFilterComposesWithScope(t *testing.T) {
	l := hosts.NewList(sampleEntries())
	l.SetScope("vw:personal") // gitlab-runner, ci-box
	l.SetFilter("ci")         // ci-box
	if got := l.Visible(); len(got) != 1 || got[0].Alias != "ci-box" {
		t.Errorf("scope+filter: got %+v, want [ci-box]", got)
	}
}

// TestLiveMarking: MarkLive/MarkDead toggles the green-dot Live flag, and the
// flag survives filtering (Q18/iii live-session indicator).
func TestLiveMarking(t *testing.T) {
	l := hosts.NewList(sampleEntries())
	l.MarkLive("prod-db-01", "file")
	l.MarkLive("ci-box", "vw:personal")
	vis := l.Visible()
	live := liveAliases(vis)
	if !contains(live, "prod-db-01") || !contains(live, "ci-box") {
		t.Errorf("live set = %v, want both prod-db-01 and ci-box", live)
	}
	l.MarkDead("prod-db-01", "file")
	vis = l.Visible()
	if contains(liveAliases(vis), "prod-db-01") {
		t.Errorf("prod-db-01 still live after MarkDead: %v", liveAliases(vis))
	}
	if !contains(liveAliases(vis), "ci-box") {
		t.Errorf("ci-box lost live flag after unrelated MarkDead: %v", liveAliases(vis))
	}
}

// TestSetScopeUnknownIsNoop: an unknown scope falls back to "all" safely.
func TestSetScopeUnknownIsNoop(t *testing.T) {
	l := hosts.NewList(sampleEntries())
	l.SetScope("vw:nonexistent")
	if got := len(l.Visible()); got != 5 {
		t.Errorf("unknown scope: got %d, want all 5", got)
	}
	if l.Scope() != "" {
		t.Errorf("unknown scope should fall back to all (\"\"); got %q", l.Scope())
	}
}

func eqStr(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func liveAliases(es []hosts.Entry) []string {
	var out []string
	for _, e := range es {
		if e.Live {
			out = append(out, e.Alias)
		}
	}
	return out
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// TestMergeAppendsVaultEntriesAndRecomputesScopes: after Merge, new entries
// appear in All() and the scope cycle includes the new vault source.
func TestMergeAppendsVaultEntriesAndRecomputesScopes(t *testing.T) {
	l := hosts.NewList([]hosts.Entry{
		{Alias: "prod-db-01", HostName: "10.0.0.5", Source: "file"},
	})

	// Before merge: only file scope.
	scopes := l.Scopes()
	if !contains(scopes, "file") || contains(scopes, "vw:personal") {
		t.Fatalf("pre-merge scopes = %v", scopes)
	}

	// Merge vault entries.
	l.Merge([]hosts.Entry{
		{Alias: "gitlab", HostName: "10.1.0.9", Source: "vw:personal"},
		{Alias: "ci-box", HostName: "10.1.0.10", Source: "vw:personal"},
	})

	// After merge: vault entries present + scope includes vw:personal.
	all := l.All()
	if len(all) != 3 {
		t.Fatalf("expected 3 entries after merge, got %d", len(all))
	}
	scopes = l.Scopes()
	if !contains(scopes, "vw:personal") {
		t.Errorf("post-merge scopes missing vw:personal: %v", scopes)
	}
}

// TestMergePreservesLiveFlags: live sessions on existing entries survive merge.
func TestMergePreservesLiveFlags(t *testing.T) {
	l := hosts.NewList([]hosts.Entry{
		{Alias: "prod-db-01", HostName: "10.0.0.5", Source: "file"},
	})
	l.MarkLive("prod-db-01", "file")

	l.Merge([]hosts.Entry{{Alias: "vault-host", HostName: "10.1.0.9", Source: "vw:personal"}})

	for _, e := range l.All() {
		if e.Alias == "prod-db-01" && e.Source == "file" && !e.Live {
			t.Error("live flag lost on prod-db-01 after merge")
		}
	}
}

// TestMergeResetsFilter: merge re-applies the current filter to include new entries.
func TestMergeResetsFilter(t *testing.T) {
	l := hosts.NewList([]hosts.Entry{
		{Alias: "prod-db-01", HostName: "10.0.0.5", Source: "file"},
	})
	l.SetFilter("git")

	// Before merge: filter matches nothing.
	if vis := l.Visible(); len(vis) != 0 {
		t.Fatalf("pre-merge visible = %d, want 0", len(vis))
	}

	l.Merge([]hosts.Entry{{Alias: "gitlab", HostName: "10.1.0.9", Source: "vw:personal"}})

	// After merge: filter matches the new vault entry.
	vis := l.Visible()
	if len(vis) != 1 || vis[0].Alias != "gitlab" {
		t.Errorf("post-merge visible = %v, want [gitlab]", vis)
	}
}

// TestListReplaceVaultEntriesPreservesLiveState: ReplaceVaultEntries replaces
// all entries for the specified vault source with newEntries, preserving
// existing Live session flags for matching aliases.
func TestListReplaceVaultEntriesPreservesLiveState(t *testing.T) {
	l := hosts.NewList([]hosts.Entry{
		{Alias: "prod-db-01", HostName: "10.0.0.5", Source: "vw:personal"},
		{Alias: "web-02", HostName: "web-02.internal", Source: "vw:personal"},
		{Alias: "bastion-prod", HostName: "b.internal", Source: "vw:work"},
	})

	l.MarkLive("prod-db-01", "vw:personal")

	newEntries := []hosts.Entry{
		{Alias: "prod-db-01", HostName: "10.0.0.5", Source: "vw:personal"},
		{Alias: "api-gw", HostName: "10.0.0.99", Source: "vw:personal"},
	}

	l.ReplaceVaultEntries("vw:personal", newEntries)

	all := l.All()
	if len(all) != 3 {
		t.Fatalf("got %d entries after replace, want 3", len(all))
	}

	foundProdDB := false
	for _, e := range all {
		if e.Alias == "prod-db-01" && e.Source == "vw:personal" {
			foundProdDB = true
			if !e.Live {
				t.Errorf("prod-db-01 lost Live flag after ReplaceVaultEntries")
			}
		}
		if e.Alias == "web-02" {
			t.Errorf("web-02 should have been removed by ReplaceVaultEntries")
		}
	}
	if !foundProdDB {
		t.Errorf("prod-db-01 missing from All()")
	}
}

func TestListRemoveEntry(t *testing.T) {
	l := hosts.NewList([]hosts.Entry{
		{Alias: "host1", HostName: "1.1.1.1", Source: "file"},
		{Alias: "host2", HostName: "2.2.2.2", Source: "vw"},
	})

	l.Remove("host1", "file")

	all := l.All()
	if len(all) != 1 || all[0].Alias != "host2" {
		t.Errorf("All() = %v, want [host2]", all)
	}
}