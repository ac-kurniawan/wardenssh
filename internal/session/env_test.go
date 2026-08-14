package session_test

import (
	"testing"

	"github.com/ac-kurniawan/wardenssh/internal/session"
)

// TestMergeEnvOverridesInheritedVars: keys named in extra must win over the
// parent environment on all platforms — the inherited entries for those keys
// are stripped and the extra values appended exactly once, while unrelated
// inherited vars survive. (On Unix os/exec keeps the FIRST occurrence, so a
// stale SSH_ASKPASS / SSH_AUTH_SOCK in the ambient env would otherwise win
// over the values WardenSSH injects.)
func TestMergeEnvOverridesInheritedVars(t *testing.T) {
	// Inherited ambient values that must NOT leak through.
	t.Setenv("SSH_AUTH_SOCK", "ambient-sock")
	t.Setenv("SSH_ASKPASS", "ambient-helper")
	t.Setenv("WARDENSSH_ASKPASS_PASS", "ambient-secret")
	t.Setenv("PATH", "/usr/bin:/bin")

	merged := session.MergeEnv([]string{
		"SSH_ASKPASS=warden-helper",
		"SSH_AUTH_SOCK=warden-pipe",
		"WARDENSSH_ASKPASS_PASS=hunter2",
		"UNRELATED=keep",
	})

	// The provided values must appear exactly once each.
	want := map[string]string{
		"SSH_ASKPASS":            "warden-helper",
		"SSH_AUTH_SOCK":          "warden-pipe",
		"WARDENSSH_ASKPASS_PASS": "hunter2",
		"UNRELATED":              "keep",
	}
	for key, val := range want {
		count := countEnv(merged, key+"="+val)
		if count != 1 {
			t.Errorf("key %s: found %d occurrence(s) of %q, want exactly 1", key, count, val)
		}
	}

	// The inherited values for the overridden keys must not appear anywhere.
	notWant := map[string]string{
		"SSH_AUTH_SOCK":          "ambient-sock",
		"SSH_ASKPASS":            "ambient-helper",
		"WARDENSSH_ASKPASS_PASS": "ambient-secret",
	}
	for key, val := range notWant {
		if count := countEnv(merged, key+"="+val); count != 0 {
			t.Errorf("inherited %s=%q leaked into merged env (%d occurrence(s))", key, val, count)
		}
	}

	// Unrelated inherited vars (e.g. PATH) are preserved.
	if count := countEnv(merged, "PATH=/usr/bin:/bin"); count != 1 {
		t.Errorf("PATH = %d occurrence(s), want exactly 1 (inherited env must be preserved)", count)
	}
}

// countEnv returns how many entries of merged match the exact key=value pair.
func countEnv(merged []string, kv string) int {
	n := 0
	for _, e := range merged {
		if e == kv {
			n++
		}
	}
	return n
}

// TestMergeEnvStripsOnlyNamedKeys: a key that exists in the parent env but is
// NOT in extra must survive untouched.
func TestMergeEnvKeepsUnrelatedInheritedKeys(t *testing.T) {
	t.Setenv("HOME", "/home/warden")
	merged := session.MergeEnv(nil)
	if count := countEnv(merged, "HOME=/home/warden"); count != 1 {
		t.Errorf("HOME = %d occurrence(s), want 1", count)
	}
}
