package sshconfig_test

import (
	"strings"
	"testing"

	"github.com/ac-kurniawan/wardenssh/internal/sshconfig"
)

// Fixture: concrete hosts, a jump host, a wildcard-defaults block, and a
// multi-pattern Host block. Used across several assertions.
const fixture = `# top comment
Host prod-db-01
    HostName 10.0.0.5
    User admin
    Port 2222
    IdentityFile ~/.ssh/id_ed25519

Host web-02
    HostName web-02.internal
    ProxyJump bastion

Host bastion web-jump
    HostName bastion.internal
    User jumpuser
    Port 22

Host *
    User defaultuser
    Port 22
`

// TestParseExtractsConcreteHosts: non-wildcard Host blocks are returned with
// their connection directives (HostName/User/Port/ProxyJump/IdentityFile).
func TestParseExtractsConcreteHosts(t *testing.T) {
	hosts, err := sshconfig.Parse(strings.NewReader(fixture))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byAlias := map[string]sshconfig.Host{}
	for _, h := range hosts {
		byAlias[h.Alias] = h
	}
	pd, ok := byAlias["prod-db-01"]
	if !ok {
		t.Fatal("missing prod-db-01")
	}
	if pd.HostName != "10.0.0.5" {
		t.Errorf("HostName = %q, want 10.0.0.5", pd.HostName)
	}
	if pd.User != "admin" {
		t.Errorf("User = %q, want admin", pd.User)
	}
	if pd.Port != "2222" {
		t.Errorf("Port = %q, want 2222", pd.Port)
	}
	if pd.IdentityFile != "~/.ssh/id_ed25519" {
		t.Errorf("IdentityFile = %q, want ~/.ssh/id_ed25519", pd.IdentityFile)
	}
	if pd.ProxyJump != "" {
		t.Errorf("ProxyJump = %q, want empty", pd.ProxyJump)
	}
}

// TestParseInheritsFromWildcard: a concrete block without an explicit User/Port
// inherits the value from the Host * wildcard defaults (Q27/C delegates the
// grammar, including inheritance, to kevinburke/ssh_config).
func TestParseInheritsFromWildcard(t *testing.T) {
	hosts, err := sshconfig.Parse(strings.NewReader(fixture))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byAlias := map[string]sshconfig.Host{}
	for _, h := range hosts {
		byAlias[h.Alias] = h
	}
	w, ok := byAlias["web-02"]
	if !ok {
		t.Fatal("missing web-02")
	}
	if w.User != "defaultuser" {
		t.Errorf("User = %q, want defaultuser (inherited from Host *)", w.User)
	}
	if w.Port != "22" {
		t.Errorf("Port = %q, want 22 (inherited from Host *)", w.Port)
	}
	if w.ProxyJump != "bastion" {
		t.Errorf("ProxyJump = %q, want bastion", w.ProxyJump)
	}
	if w.HostName != "web-02.internal" {
		t.Errorf("HostName = %q, want web-02.internal", w.HostName)
	}
}

// TestParseFlagsWildcard: every Host block whose pattern contains '*' is
// flagged Wildcard=true (kevinburke injects an implicit 'Host *' at the top,
// so there may be more than one), and no non-wildcard block is mis-flagged.
func TestParseFlagsWildcard(t *testing.T) {
	hosts, err := sshconfig.Parse(strings.NewReader(fixture))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	sawExplicitStar := false
	for _, h := range hosts {
		mustWild := strings.Contains(h.Alias, "*")
		if h.Wildcard != mustWild {
			t.Errorf("alias %q: Wildcard=%v, want %v", h.Alias, h.Wildcard, mustWild)
		}
		if h.Alias == "*" {
			sawExplicitStar = true
		}
	}
	if !sawExplicitStar {
		t.Error("explicit 'Host *' block from fixture not found among entries")
	}
}

// TestParseMultiPatternAlias: a Host block with multiple patterns stores the
// joined alias as the display label (Q11/C: each entry shown, labeled by
// source) and resolves the first pattern.
func TestParseMultiPatternAlias(t *testing.T) {
	hosts, err := sshconfig.Parse(strings.NewReader(fixture))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byAlias := map[string]sshconfig.Host{}
	for _, h := range hosts {
		byAlias[h.Alias] = h
	}
	bastion, ok := byAlias["bastion web-jump"]
	if !ok {
		t.Fatalf("missing multi-pattern entry; got aliases %v", keys(byAlias))
	}
	if bastion.HostName != "bastion.internal" {
		t.Errorf("HostName = %q, want bastion.internal", bastion.HostName)
	}
	if bastion.User != "jumpuser" {
		t.Errorf("User = %q, want jumpuser", bastion.User)
	}
}

func keys(m map[string]sshconfig.Host) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}