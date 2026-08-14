package sshconfig_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ac-kurniawan/wardenssh/internal/sshconfig"
	"golang.org/x/crypto/ssh"
)

func TestGenerateKeyToFile_Ed25519(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "id_ed25519_test")

	err := sshconfig.GenerateKeyToFile("ed25519", keyPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	privBytes, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("failed reading private key: %v", err)
	}
	if !strings.Contains(string(privBytes), "OPENSSH PRIVATE KEY") {
		t.Errorf("expected OPENSSH PRIVATE KEY format, got: %s", string(privBytes))
	}

	// Verify private key parses cleanly as OpenSSH raw private key
	_, err = ssh.ParseRawPrivateKey(privBytes)
	if err != nil {
		t.Fatalf("failed parsing private key: %v", err)
	}

	pubBytes, err := os.ReadFile(keyPath + ".pub")
	if err != nil {
		t.Fatalf("failed reading public key: %v", err)
	}
	if !strings.HasPrefix(string(pubBytes), "ssh-ed25519 ") {
		t.Errorf("expected ssh-ed25519 public key, got: %s", string(pubBytes))
	}

	_, _, _, _, err = ssh.ParseAuthorizedKey(pubBytes)
	if err != nil {
		t.Fatalf("failed parsing authorized public key: %v", err)
	}

	if runtime.GOOS != "windows" {
		info, err := os.Stat(keyPath)
		if err != nil {
			t.Fatalf("failed stat on private key: %v", err)
		}
		if info.Mode().Perm() != 0600 {
			t.Errorf("expected 0600 permissions, got %o", info.Mode().Perm())
		}

		pubInfo, err := os.Stat(keyPath + ".pub")
		if err != nil {
			t.Fatalf("failed stat on public key: %v", err)
		}
		if pubInfo.Mode().Perm() != 0644 {
			t.Errorf("expected 0644 permissions, got %o", pubInfo.Mode().Perm())
		}
	}
}

func TestGenerateKeyToFile_RSA(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "id_rsa_test")

	err := sshconfig.GenerateKeyToFile("rsa", keyPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	privBytes, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("failed reading private key: %v", err)
	}
	if !strings.Contains(string(privBytes), "OPENSSH PRIVATE KEY") && !strings.Contains(string(privBytes), "RSA PRIVATE KEY") {
		t.Errorf("expected private key PEM format, got: %s", string(privBytes))
	}

	_, err = ssh.ParseRawPrivateKey(privBytes)
	if err != nil {
		t.Fatalf("failed parsing private key: %v", err)
	}

	pubBytes, err := os.ReadFile(keyPath + ".pub")
	if err != nil {
		t.Fatalf("failed reading public key: %v", err)
	}
	if !strings.HasPrefix(string(pubBytes), "ssh-rsa ") {
		t.Errorf("expected ssh-rsa public key, got: %s", string(pubBytes))
	}

	_, _, _, _, err = ssh.ParseAuthorizedKey(pubBytes)
	if err != nil {
		t.Fatalf("failed parsing authorized public key: %v", err)
	}
}

func TestAppendHostEntry(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config")

	cfg := sshconfig.HostConfig{
		Alias:        "myserver",
		HostName:     "192.168.1.50",
		User:         "ubuntu",
		Port:         "2222",
		ProxyJump:    "bastion",
		IdentityFile: "~/.ssh/id_ed25519_myserver",
	}

	err := sshconfig.AppendHostEntry(configPath, cfg)
	if err != nil {
		t.Fatalf("failed appending host entry: %v", err)
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed reading config: %v", err)
	}

	str := string(content)
	expectedLines := []string{
		"Host myserver",
		"    HostName 192.168.1.50",
		"    User ubuntu",
		"    Port 2222",
		"    ProxyJump bastion",
		"    IdentityFile ~/.ssh/id_ed25519_myserver",
	}

	for _, line := range expectedLines {
		if !strings.Contains(str, line) {
			t.Errorf("expected config to contain %q, content:\n%s", line, str)
		}
	}

	// Append a second entry to verify append behavior
	cfg2 := sshconfig.HostConfig{
		Alias:    "secondserver",
		HostName: "10.0.0.1",
	}
	err = sshconfig.AppendHostEntry(configPath, cfg2)
	if err != nil {
		t.Fatalf("failed appending second host entry: %v", err)
	}

	content2, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed reading updated config: %v", err)
	}
	str2 := string(content2)
	if !strings.Contains(str2, "Host myserver") || !strings.Contains(str2, "Host secondserver") {
		t.Errorf("expected both host entries in config:\n%s", str2)
	}
	if strings.Contains(str2, "Host secondserver\n    User") {
		t.Errorf("did not expect User field for secondserver:\n%s", str2)
	}
}

func TestGenerateKeyToFile_RSA4096_And_NestedDir(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "nested", "keys", "id_rsa4096")

	err := sshconfig.GenerateKeyToFile("rsa4096", keyPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	privBytes, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("failed reading private key: %v", err)
	}
	_, err = ssh.ParseRawPrivateKey(privBytes)
	if err != nil {
		t.Fatalf("failed parsing private key: %v", err)
	}
}

func TestAppendHostEntry_ParseRoundTrip(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "nested", "ssh", "config")

	cfg := sshconfig.HostConfig{
		Alias:        "roundtrip-node",
		HostName:     "10.10.10.10",
		User:         "deploy",
		Port:         "2200",
		ProxyJump:    "jump-node",
		IdentityFile: "~/.ssh/id_ed25519_deploy",
	}

	err := sshconfig.AppendHostEntry(configPath, cfg)
	if err != nil {
		t.Fatalf("AppendHostEntry failed: %v", err)
	}

	f, err := os.Open(configPath)
	if err != nil {
		t.Fatalf("open config: %v", err)
	}
	defer f.Close()

	hosts, err := sshconfig.Parse(f)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	var found *sshconfig.Host
	for i := range hosts {
		if hosts[i].Alias == "roundtrip-node" {
			found = &hosts[i]
			break
		}
	}

	if found == nil {
		t.Fatalf("roundtrip-node not found in parsed hosts: %+v", hosts)
	}
	if found.HostName != "10.10.10.10" {
		t.Errorf("HostName = %q, want 10.10.10.10", found.HostName)
	}
	if found.User != "deploy" {
		t.Errorf("User = %q, want deploy", found.User)
	}
	if found.Port != "2200" {
		t.Errorf("Port = %q, want 2200", found.Port)
	}
	if found.ProxyJump != "jump-node" {
		t.Errorf("ProxyJump = %q, want jump-node", found.ProxyJump)
	}
	if found.IdentityFile != "~/.ssh/id_ed25519_deploy" {
		t.Errorf("IdentityFile = %q, want ~/.ssh/id_ed25519_deploy", found.IdentityFile)
	}
}

func TestDeleteHostEntry(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config")

	_ = sshconfig.AppendHostEntry(configPath, sshconfig.HostConfig{
		Alias:    "host1",
		HostName: "1.1.1.1",
	})
	_ = sshconfig.AppendHostEntry(configPath, sshconfig.HostConfig{
		Alias:    "host2",
		HostName: "2.2.2.2",
	})
	_ = sshconfig.AppendHostEntry(configPath, sshconfig.HostConfig{
		Alias:    "host3",
		HostName: "3.3.3.3",
	})

	err := sshconfig.DeleteHostEntry(configPath, "host2")
	if err != nil {
		t.Fatalf("DeleteHostEntry failed: %v", err)
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}

	str := string(content)
	if !strings.Contains(str, "Host host1") || !strings.Contains(str, "Host host3") {
		t.Errorf("expected host1 and host3 to remain in config:\n%s", str)
	}
	if strings.Contains(str, "Host host2") || strings.Contains(str, "2.2.2.2") {
		t.Errorf("expected host2 block to be deleted, but found in config:\n%s", str)
	}
}


