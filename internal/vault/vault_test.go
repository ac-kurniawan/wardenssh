package vault_test

import (
	"testing"

	"github.com/ac-kurniawan/wardenssh/internal/vault"
)

// TestFakeSourceFiltersByHostField: per Q32/B, only SSH-Key items with a
// populated 'host' custom field appear as launchable hosts. The stub filters
// accordingly (real vault client will apply the same convention via the
// configurable custom_fields.host name from config).
func TestFakeSourceFiltersByHostField(t *testing.T) {
	src := vault.NewFakeSource("vw:personal", []vault.Item{
		{Name: "prod-db-01", HostName: "10.0.0.5", User: "admin"}, // has host -> included
		{Name: "signing-key", HostName: ""},                         // no host -> excluded
		{Name: "ci-box", HostName: "10.1.0.10", Port: "2222"},       // has host -> included
	})
	items, err := src.Items()
	if err != nil {
		t.Fatalf("Items: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2 (only items with a populated host field)", len(items))
	}
	got := map[string]bool{}
	for _, it := range items {
		got[it.Name] = true
	}
	if !got["prod-db-01"] || !got["ci-box"] {
		t.Errorf("items = %v, want prod-db-01 and ci-box", got)
	}
	if got["signing-key"] {
		t.Error("signing-key (no host) should be excluded")
	}
}

// TestFakeClientSources: the aggregate client exposes all configured sources by
// name (drives the multi-vault host-list merge Q10/C, Q16/B).
func TestFakeClientSources(t *testing.T) {
	c := vault.NewFakeClient(
		vault.NewFakeSource("vw:personal", []vault.Item{{Name: "a", HostName: "h"}}),
		vault.NewFakeSource("vw:work", []vault.Item{{Name: "b", HostName: "h"}}),
	)
	srcs := c.Sources()
	if len(srcs) != 2 {
		t.Fatalf("got %d sources, want 2", len(srcs))
	}
	if srcs[0].Name() != "vw:personal" || srcs[1].Name() != "vw:work" {
		t.Errorf("source names = %q/%q", srcs[0].Name(), srcs[1].Name())
	}
}

// TestFakeSourceEmptyHostIsIdempotent: a source with zero qualifying items
// returns an empty slice, not nil, so callers can range without nil checks.
func TestFakeSourceEmptyHostIsIdempotent(t *testing.T) {
	src := vault.NewFakeSource("vw:empty", []vault.Item{
		{Name: "no-host", HostName: ""},
	})
	items, err := src.Items()
	if err != nil {
		t.Fatalf("Items: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("got %d items, want 0", len(items))
	}
}