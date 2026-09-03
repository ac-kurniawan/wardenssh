package auth

import (
	"encoding/json"
	"testing"
)

// TestClaimsRoleKeys documents the real Zitadel v4 role-claim shape observed
// during the spike: a namespaced claim "urn:zitadel:iam:org:project:roles"
// mapping roleKey -> {orgId: domain}. RoleKeys() must flatten it deterministically.
func TestClaimsRoleKeys(t *testing.T) {
	raw := []byte(`{
		"sub": "u-123",
		"urn:zitadel:iam:org:project:roles": {
			"viewer":        {"389092263973683203": "acme-corp.localhost"},
			"asset-manager": {"389092263973683203": "acme-corp.localhost"}
		}
	}`)
	var c Claims
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got := c.RoleKeys()
	want := []string{"asset-manager", "viewer"} // sorted
	if len(got) != len(want) {
		t.Fatalf("RoleKeys() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("RoleKeys() = %v, want %v", got, want)
		}
	}
}

func TestClaimsNoRoles(t *testing.T) {
	var c Claims
	if err := json.Unmarshal([]byte(`{"sub":"u-1"}`), &c); err != nil {
		t.Fatal(err)
	}
	if keys := c.RoleKeys(); len(keys) != 0 {
		t.Fatalf("expected no roles, got %v", keys)
	}
}

func TestS256Challenge(t *testing.T) {
	// RFC 7636 appendix B vector.
	v := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	want := "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	if got := s256Challenge(v); got != want {
		t.Fatalf("s256Challenge = %s, want %s", got, want)
	}
}

func TestJSONClaims(t *testing.T) {
	// header.payload.signature — payload {"a":1}
	raw := "eyJhbGciOiJIUzI1NiJ9.eyJhIjoxfQ.sig"
	out, err := JSONClaims(raw)
	if err != nil {
		t.Fatalf("JSONClaims: %v", err)
	}
	if out != "{\n  \"a\": 1\n}" {
		t.Fatalf("unexpected pretty output: %q", out)
	}
	if _, err := JSONClaims("not-a-jwt"); err == nil {
		t.Fatal("expected error for non-JWT")
	}
}
