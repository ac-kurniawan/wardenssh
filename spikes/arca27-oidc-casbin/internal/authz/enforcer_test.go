package authz_test

import (
	"testing"

	"github.com/ac-kurniawan/wardenssh/spikes/arca27-oidc-casbin/internal/authz"
)

const (
	modelPath  = "../../deploy/model.conf"
	policyPath = "../../deploy/policy.csv"
)

func newTestEnforcer(t *testing.T) *authz.Enforcer {
	t.Helper()
	en, err := authz.NewEnforcer(modelPath, policyPath)
	if err != nil {
		t.Fatalf("new enforcer: %v", err)
	}
	return en
}

// TestRolePermissionMatrix verifies the seeded role->permission matrix.
func TestRolePermissionMatrix(t *testing.T) {
	en := newTestEnforcer(t)

	// admin: everything
	en.EnsureBinding("u-admin", []string{"admin"})
	// accountant: accounting.* + read assets
	en.EnsureBinding("u-accountant", []string{"accountant"})
	// asset-manager: assets.*
	en.EnsureBinding("u-asset-manager", []string{"asset-manager"})
	// viewer: *.read only
	en.EnsureBinding("u-viewer", []string{"viewer"})

	cases := []struct {
		name  string
		sub   string
		perm  string
		allow bool
	}{
		{"admin post journal", "u-admin", "accounting.journal.post", true},
		{"admin wildcard on anything", "u-admin", "hr.employee.delete", true},
		{"accountant post journal", "u-accountant", "accounting.journal.post", true},
		{"accountant read asset", "u-accountant", "assets.asset.read", true},
		{"accountant create asset", "u-accountant", "assets.asset.create", false},
		{"asset-manager create asset", "u-asset-manager", "assets.asset.create", true},
		{"asset-manager read journal", "u-asset-manager", "accounting.journal.read", false},
		{"viewer read asset", "u-viewer", "assets.asset.read", true},
		{"viewer read journal", "u-viewer", "accounting.journal.read", true},
		{"viewer read platform", "u-viewer", "platform.projects.read", true},
		{"viewer create asset (the 403 demo)", "u-viewer", "assets.asset.create", false},
		{"viewer post journal (the PRD 403)", "u-viewer", "accounting.journal.post", false},
		{"unknown subject denied", "nobody", "assets.asset.read", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := en.Enforce(tc.sub, tc.perm)
			if err != nil {
				t.Fatalf("enforce: %v", err)
			}
			if got != tc.allow {
				t.Fatalf("Enforce(%s, %s) = %v, want %v", tc.sub, tc.perm, got, tc.allow)
			}
		})
	}
}

// TestEnsureBindingIdempotent documents that repeated logins do not duplicate
// g rules.
func TestEnsureBindingIdempotent(t *testing.T) {
	en := newTestEnforcer(t)
	en.EnsureBinding("u-viewer", []string{"viewer"})
	en.EnsureBinding("u-viewer", []string{"viewer"})
	allowed, err := en.Enforce("u-viewer", "assets.asset.read")
	if err != nil || !allowed {
		t.Fatalf("viewer read after double binding: allowed=%v err=%v", allowed, err)
	}
	denied, err := en.Enforce("u-viewer", "assets.asset.create")
	if err != nil || denied {
		t.Fatalf("viewer create after double binding: denied=%v err=%v", denied, err)
	}
}
