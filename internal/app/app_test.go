package app_test

import (
	"strings"
	"testing"

	"github.com/ac-kurniawan/wardenssh/internal/app"
	"github.com/ac-kurniawan/wardenssh/internal/hosts"
	"github.com/ac-kurniawan/wardenssh/internal/vault"
)

const sshFixture = `Host prod-db-01
    HostName 10.0.0.5
    User admin
    Port 2222

Host web-02
    HostName web-02.internal

Host *
    User defaultuser
`

// TestBuildHostListMergesFileAndVault: file entries + vault entries appear
// together, each tagged with its source label (Q10/C dual-source, Q11/C no dedup).
func TestBuildHostListMergesFileAndVault(t *testing.T) {
	vc := vault.NewFakeClient(
		vault.NewFakeSource("vw:personal", []vault.Item{
			{Name: "ci-box", HostName: "10.1.0.10", Port: "2222"},
		}),
		vault.NewFakeSource("vw:work", []vault.Item{
			{Name: "bastion", HostName: "b.internal", ProxyJump: "jump"},
		}),
	)

	l, err := app.BuildHostList(strings.NewReader(sshFixture), vc)
	if err != nil {
		t.Fatalf("BuildHostList: %v", err)
	}
	byKey := map[string]hosts.Entry{}
	for _, e := range l.All() {
		byKey[e.Alias+"|"+e.Source] = e
	}
	expect := map[string]string{
		"prod-db-01|file":      "file",
		"web-02|file":          "file",
		"ci-box|vw:personal":   "vw:personal",
		"bastion|vw:work":      "vw:work",
	}
	for k, wantSrc := range expect {
		e, ok := byKey[k]
		if !ok {
			t.Errorf("missing entry %q", k)
			continue
		}
		if e.Source != wantSrc {
			t.Errorf("entry %q Source = %q, want %q", k, e.Source, wantSrc)
		}
	}
	if len(byKey) != len(expect) {
		t.Errorf("total entries = %d, want %d (extra/missing: %v)", len(byKey), len(expect), byKey)
	}
}

// TestBuildHostListExcludesWildcard: the Host * block does not produce a
// launchable entry (it carries defaults, not an endpoint).
func TestBuildHostListExcludesWildcard(t *testing.T) {
	l, err := app.BuildHostList(strings.NewReader(sshFixture), nil)
	if err != nil {
		t.Fatalf("BuildHostList: %v", err)
	}
	for _, e := range l.All() {
		if strings.Contains(e.Alias, "*") {
			t.Errorf("wildcard entry leaked into list: %+v", e)
		}
	}
}

// TestBuildHostListNilReaderOnlyVault: no ~/.ssh/config (nil reader) yields
// only vault entries, no error.
func TestBuildHostListNilReaderOnlyVault(t *testing.T) {
	vc := vault.NewFakeClient(
		vault.NewFakeSource("vw:personal", []vault.Item{{Name: "ci", HostName: "h"}}),
	)
	l, err := app.BuildHostList(nil, vc)
	if err != nil {
		t.Fatalf("BuildHostList: %v", err)
	}
	if len(l.All()) != 1 || l.All()[0].Source != "vw:personal" {
		t.Errorf("entries = %+v, want 1 vw:personal", l.All())
	}
}

// TestBuildHostListInheritsFromWildcard: file entry web-02 (no User in its
// block) inherits User from Host * (sshconfig delegates inheritance).
func TestBuildHostListInheritsFromWildcard(t *testing.T) {
	l, err := app.BuildHostList(strings.NewReader(sshFixture), nil)
	if err != nil {
		t.Fatalf("BuildHostList: %v", err)
	}
	for _, e := range l.All() {
		if e.Alias == "web-02" && e.User != "defaultuser" {
			t.Errorf("web-02 User = %q, want defaultuser (inherited from Host *)", e.User)
		}
	}
}

// TestBuildHostListPropagatesAuthKind: vault login items become "password"
// hosts; SSH-Key items become "key" hosts.
func TestBuildHostListPropagatesAuthKind(t *testing.T) {
	vc := vault.NewFakeClient(
		vault.NewFakeSource("vw:personal", []vault.Item{
			{Name: "ci-box", HostName: "10.1.0.10", Kind: "sshkey"},
			{Name: "prod-db", HostName: "10.0.0.9", Kind: "login"},
		}),
	)
	l, err := app.BuildHostList(nil, vc)
	if err != nil {
		t.Fatalf("BuildHostList: %v", err)
	}
	got := map[string]string{}
	for _, e := range l.All() {
		got[e.Alias] = e.AuthKind
	}
	if got["ci-box"] != "key" {
		t.Errorf("ci-box AuthKind = %q, want key", got["ci-box"])
	}
	if got["prod-db"] != "password" {
		t.Errorf("prod-db AuthKind = %q, want password", got["prod-db"])
	}
}
