package connect

import (
	"errors"
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
			name: "Basic host only",
			entry: hosts.Entry{
				Alias:    "myhost",
				HostName: "1.2.3.4",
				Source:   "file",
			},
			expected: []string{"ssh", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=accept-new", "1.2.3.4"},
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
			expected: []string{"ssh", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=accept-new", "-p", "2222", "ubuntu@1.2.3.4"},
		},
		{
			name: "Host with proxyjump",
			entry: hosts.Entry{
				Alias:     "myhost",
				HostName:  "1.2.3.4",
				ProxyJump: "jumpbox",
				Source:    "vw:personal",
			},
			expected: []string{"ssh", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=accept-new", "-J", "jumpbox", "1.2.3.4"},
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

type fakeClient struct {
	sources []vault.Source
}

func (f *fakeClient) Sources() []vault.Source { return f.sources }

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
