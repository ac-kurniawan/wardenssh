package connect

import (
	"errors"
	"strings"
	"testing"

	"github.com/ac-kurniawan/wardenssh/internal/hosts"
	"github.com/ac-kurniawan/wardenssh/internal/vault"
)

func TestSSHArgv(t *testing.T) {
	// Backup original SSHBin
	origSSHBin := SSHBin
	defer func() { SSHBin = origSSHBin }()
	SSHBin = "ssh"

	tests := []struct {
		name     string
		entry    hosts.Entry
		expected []string
	}{
		{
			name: "Basic host only — defaults to root",
			entry: hosts.Entry{
				Alias:    "myhost",
				HostName: "1.2.3.4",
				Source:   "file",
			},
			expected: []string{"ssh", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=accept-new", "-o", "ServerAliveInterval=15", "-o", "ServerAliveCountMax=3", "root@1.2.3.4"},
		},
		{
			name: "Host with user and port",
			entry: hosts.Entry{
				Alias:    "myhost",
				HostName: "1.2.3.4",
				User:     "ubuntu",
				Port:     "2222",
				Source:   "vw:personal",
			},
			expected: []string{"ssh", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=accept-new", "-o", "ServerAliveInterval=15", "-o", "ServerAliveCountMax=3", "-p", "2222", "ubuntu@1.2.3.4"},
		},
		{
			name: "Host with proxyjump — defaults to root",
			entry: hosts.Entry{
				Alias:     "myhost",
				HostName:  "1.2.3.4",
				ProxyJump: "jumpbox",
				Source:    "vw:personal",
			},
			expected: []string{"ssh", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=accept-new", "-o", "ServerAliveInterval=15", "-o", "ServerAliveCountMax=3", "-J", "jumpbox", "root@1.2.3.4"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SSHArgv(tc.entry, "/tmp/agent.sock")
			if len(got) != len(tc.expected) {
				t.Fatalf("expected argv len %v, got %v\ngot: %v", len(tc.expected), len(got), got)
			}
			for i := range got {
				if got[i] != tc.expected[i] {
					t.Errorf("argv mismatch at index %d: expected %q, got %q", i, tc.expected[i], got[i])
				}
			}
		})
	}
}

func TestSSHArgvKeepaliveOptions(t *testing.T) {
	origSSHBin := SSHBin
	defer func() { SSHBin = origSSHBin }()
	SSHBin = "ssh"

	tests := []struct {
		name string
		got  []string
	}{
		{"key-auth", SSHArgv(hosts.Entry{Alias: "h", HostName: "h.internal", Source: "file"}, "/tmp/agent.sock")},
		{"password-auth", SSHArgvPassword(hosts.Entry{Alias: "h", HostName: "h.internal", Source: "vw:personal", AuthKind: "password"})},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			foundInterval := false
			foundCount := false
			for i, arg := range tc.got {
				if arg == "-o" && i+1 < len(tc.got) && tc.got[i+1] == "ServerAliveInterval=15" {
					foundInterval = true
				}
				if arg == "-o" && i+1 < len(tc.got) && tc.got[i+1] == "ServerAliveCountMax=3" {
					foundCount = true
				}
			}
			if !foundInterval {
				t.Errorf("missing ServerAliveInterval=15: %v", tc.got)
			}
			if !foundCount {
				t.Errorf("missing ServerAliveCountMax=3: %v", tc.got)
			}
		})
	}
}

func TestSSHArgvPassesIdentityFileForFileSource(t *testing.T) {
	entry := hosts.Entry{
		Alias:        "myhost",
		HostName:     "10.0.0.1",
		User:         "admin",
		Port:         "2222",
		Source:       "file",
		IdentityFile: "/home/user/.ssh/id_ed25519",
	}
	argv := SSHArgv(entry, "/tmp/agent-sock")

	// Should contain -i /home/user/.ssh/id_ed25519
	found := false
	for i, arg := range argv {
		if arg == "-i" && i+1 < len(argv) && argv[i+1] == "/home/user/.ssh/id_ed25519" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected -i /home/user/.ssh/id_ed25519 in argv, got %v", argv)
	}
}

func TestSSHArgvOmitsIdentityFileForVaultSource(t *testing.T) {
	entry := hosts.Entry{
		Alias:    "vaulthost",
		HostName: "10.0.0.2",
		User:     "root",
		Source:   "vw:personal",
	}
	argv := SSHArgv(entry, "/tmp/agent-sock")

	for i, arg := range argv {
		if arg == "-i" {
			t.Errorf("vault-sourced entry should not have -i, got %v (at index %d)", argv, i)
		}
	}
}

type fakeSource struct {
	name  string
	items []vault.Item
	err   error
}

func (f *fakeSource) Name() string                     { return f.name }
func (f *fakeSource) Items() ([]vault.Item, error)     { return f.items, f.err }
func (f *fakeSource) DecryptPrivateKey(it vault.Item, pass string) ([]byte, error) {
	if it.EncPrivateKey == "" {
		return nil, errors.New("no key")
	}
	return []byte(it.EncPrivateKey), nil
}
func (f *fakeSource) DecryptLogin(it vault.Item) ([]byte, []byte, error) {
	return []byte(it.EncUsername), []byte(it.EncPassword), nil
}
func (f *fakeSource) Sync() error { return nil }

type fakeClient struct {
	sources []vault.Source
}

func (f *fakeClient) Sources() []vault.Source { return f.sources }
func (f *fakeClient) Sync() error             { return nil }

func TestFindVaultItem(t *testing.T) {
	fc := &fakeClient{
		sources: []vault.Source{
			&fakeSource{
				name: "vw:personal",
				items: []vault.Item{
					{Name: "item1", EncPrivateKey: "key1"},
					{Name: "item2", EncPrivateKey: "key2"},
				},
			},
			&fakeSource{
				name: "vw:work",
				items: []vault.Item{
					{Name: "item3", EncPrivateKey: "key3"},
				},
			},
		},
	}

	// Success case
	it, src, err := findVaultItem(fc, hosts.Entry{Alias: "item2", Source: "vw:personal"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if it.Name != "item2" || src.Name() != "vw:personal" {
		t.Errorf("wrong item/source returned: it=%v, src=%v", it, src.Name())
	}

	// Not found case
	_, _, err = findVaultItem(fc, hosts.Entry{Alias: "nonexistent", Source: "vw:personal"})
	if err == nil {
		t.Error("expected error but got nil")
	}
}

func TestSSHArgvPassword(t *testing.T) {
	origSSHBin := SSHBin
	defer func() { SSHBin = origSSHBin }()
	SSHBin = "ssh"

	tests := []struct {
		name     string
		entry    hosts.Entry
		expected []string
	}{
		{
			name:     "password host defaults to root",
			entry:    hosts.Entry{Alias: "prod-db", HostName: "10.0.0.9", Source: "vw:personal", AuthKind: "password"},
			expected: []string{"ssh", "-o", "StrictHostKeyChecking=accept-new", "-o", "ServerAliveInterval=15", "-o", "ServerAliveCountMax=3", "root@10.0.0.9"},
		},
		{
			name:     "password host with user and port",
			entry:    hosts.Entry{Alias: "prod-db", HostName: "10.0.0.9", User: "admin", Port: "2222", Source: "vw:personal", AuthKind: "password"},
			expected: []string{"ssh", "-o", "StrictHostKeyChecking=accept-new", "-o", "ServerAliveInterval=15", "-o", "ServerAliveCountMax=3", "-p", "2222", "admin@10.0.0.9"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SSHArgvPassword(tc.entry)
			if len(got) != len(tc.expected) {
				t.Fatalf("expected argv len %v, got %v\ngot: %v", len(tc.expected), len(got), got)
			}
			for i := range got {
				if got[i] != tc.expected[i] {
					t.Errorf("argv mismatch at index %d: expected %q, got %q", i, tc.expected[i], got[i])
				}
			}
		})
	}
}

func TestSSHArgvPasswordOmitsBatchMode(t *testing.T) {
	argv := SSHArgvPassword(hosts.Entry{Alias: "h", HostName: "h.internal", Source: "vw:personal", AuthKind: "password"})
	for _, a := range argv {
		if a == "BatchMode=yes" {
			t.Errorf("password argv must not set BatchMode (forbids password auth): %v", argv)
		}
	}
}

func TestEnvForAskpass(t *testing.T) {
	orig := askpassExecutable
	askpassExecutable = func() string { return `C:\wardenssh\wardenssh.exe` }
	defer func() { askpassExecutable = orig }()

	env := EnvForAskpass(`\\.\pipe\wardenssh-agent`, "hunter2")
	want := map[string]string{
		"SSH_AUTH_SOCK":          `\\.\pipe\wardenssh-agent`,
		"SSH_ASKPASS":            `C:\wardenssh\wardenssh.exe`,
		"SSH_ASKPASS_REQUIRE":    "force",
		"WARDENSSH_ASKPASS":      "1",
		"WARDENSSH_ASKPASS_PASS": "hunter2",
	}
	if len(env) != len(want) {
		t.Fatalf("got %d env entries, want %d: %v", len(env), len(want), env)
	}
	for _, kv := range env {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) != 2 {
			t.Fatalf("bad env entry %q", kv)
		}
		if parts[1] != want[parts[0]] {
			t.Errorf("%s = %q, want %q", parts[0], parts[1], want[parts[0]])
		}
	}
}

func TestPrepareLoginCreds(t *testing.T) {
	fc := &fakeClient{sources: []vault.Source{
		&fakeSource{name: "vw:personal", items: []vault.Item{
			{Name: "prod-db", EncUsername: "admin", EncPassword: "s3cret"},
		}},
	}}
	user, pass, err := PrepareLoginCreds(hosts.Entry{Alias: "prod-db", Source: "vw:personal"}, fc)
	if err != nil {
		t.Fatalf("PrepareLoginCreds: %v", err)
	}
	if string(user) != "admin" || string(pass) != "s3cret" {
		t.Errorf("user=%q pass=%q, want admin/s3cret", user, pass)
	}
}

func TestCommandForPasswordHost(t *testing.T) {
	fc := &fakeClient{sources: []vault.Source{
		&fakeSource{name: "vw:personal", items: []vault.Item{
			{Name: "prod-db", Kind: "login", EncUsername: "admin", EncPassword: "s3cret"},
		}},
	}}
	entry := hosts.Entry{Alias: "prod-db", HostName: "10.0.0.9", Source: "vw:personal", AuthKind: "password"}
	argv, env, err := CommandFor(entry, "sess-1", `\\.\pipe\wardenssh-agent`, fc, nil)
	if err != nil {
		t.Fatalf("CommandFor: %v", err)
	}
	for _, a := range argv {
		if a == "BatchMode=yes" {
			t.Errorf("password argv must not set BatchMode: %v", argv)
		}
	}
	havePass := false
	for _, kv := range env {
		if kv == "WARDENSSH_ASKPASS_PASS=s3cret" {
			havePass = true
		}
	}
	if !havePass {
		t.Errorf("env missing WARDENSSH_ASKPASS_PASS=s3cret: %v", env)
	}
}

func TestCommandForPasswordHostWithoutVaultClient(t *testing.T) {
	entry := hosts.Entry{Alias: "prod-db", HostName: "10.0.0.9", Source: "vw:personal", AuthKind: "password"}
	if _, _, err := CommandFor(entry, "sess-1", "pipe", nil, nil); err == nil {
		t.Fatal("expected error for password host with nil vault client")
	}
}

func TestCommandForKeyHostMatchesSSHArgv(t *testing.T) {
	entry := hosts.Entry{Alias: "ci-box", HostName: "10.1.0.10", Source: "vw:personal"}
	argv, env, err := CommandFor(entry, "sess-1", "/tmp/agent.sock", nil, nil)
	if err != nil {
		t.Fatalf("CommandFor: %v", err)
	}
	want := SSHArgv(entry, "/tmp/agent.sock")
	if len(argv) != len(want) {
		t.Fatalf("argv = %v, want %v", argv, want)
	}
	for i := range argv {
		if argv[i] != want[i] {
			t.Errorf("argv[%d] = %q, want %q", i, argv[i], want[i])
		}
	}
	if len(env) != 1 || env[0] != "SSH_AUTH_SOCK=/tmp/agent.sock" {
		t.Errorf("env = %v, want SSH_AUTH_SOCK only", env)
	}
}
