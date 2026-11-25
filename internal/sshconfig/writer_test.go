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

func TestGenerateKeyPairFingerprint(t *testing.T) {
	// BitWarden SSH-Key items require a non-empty keyFingerprint
	// ("SHA256:<base64>" of the public key, same as ssh-keygen -lf).
	// VaultWarden nulls the whole sshKey when it's missing, and the web
	// vault crashes parsing an empty one.
	_, pubAuth, fp, err := sshconfig.GenerateKeyPair("ed25519")
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	if !strings.HasPrefix(fp, "SHA256:") {
		t.Fatalf("fingerprint = %q, want SHA256:... prefix", fp)
	}
	pub, _, _, _, err := ssh.ParseAuthorizedKey([]byte(pubAuth))
	if err != nil {
		t.Fatalf("ParseAuthorizedKey: %v", err)
	}
	if want := ssh.FingerprintSHA256(pub); fp != want {
		t.Errorf("fingerprint = %q, want %q", fp, want)
	}
}

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

func TestUpdateHostEntry_InPlaceAndPreserveDirectives(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config")

	initial := `Host host1
    HostName 1.1.1.1

# Custom comment before host2
Host host2
    # Inline comment inside host2
    HostName 2.2.2.2
    User olduser
    Port 2200
    ForwardAgent yes
    LocalForward 8080 127.0.0.1:80

Host host3
    HostName 3.3.3.3
`
	if err := os.WriteFile(configPath, []byte(initial), 0600); err != nil {
		t.Fatalf("write initial config: %v", err)
	}

	cfg := sshconfig.HostConfig{
		Alias:        "host2",
		HostName:     "2.2.2.99",
		User:         "newuser",
		Port:         "2222",
		ProxyJump:    "jumpbox",
		IdentityFile: "~/.ssh/id_ed25519_host2",
	}

	err := sshconfig.UpdateHostEntry(configPath, "host2", cfg)
	if err != nil {
		t.Fatalf("UpdateHostEntry failed: %v", err)
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read updated config: %v", err)
	}
	str := string(content)

	// Verify position: host1 comes before host2, host2 comes before host3
	idx1 := strings.Index(str, "Host host1")
	idx2 := strings.Index(str, "Host host2")
	idx3 := strings.Index(str, "Host host3")
	if idx1 == -1 || idx2 == -1 || idx3 == -1 || !(idx1 < idx2 && idx2 < idx3) {
		t.Errorf("expected block order host1 < host2 < host3, got indices %d, %d, %d in:\n%s", idx1, idx2, idx3, str)
	}

	// Verify updated fields
	if !strings.Contains(str, "HostName 2.2.2.99") {
		t.Errorf("expected updated HostName 2.2.2.99 in:\n%s", str)
	}
	if !strings.Contains(str, "User newuser") {
		t.Errorf("expected updated User newuser in:\n%s", str)
	}
	if !strings.Contains(str, "Port 2222") {
		t.Errorf("expected updated Port 2222 in:\n%s", str)
	}
	if !strings.Contains(str, "ProxyJump jumpbox") {
		t.Errorf("expected ProxyJump jumpbox in:\n%s", str)
	}
	if !strings.Contains(str, "IdentityFile ~/.ssh/id_ed25519_host2") {
		t.Errorf("expected IdentityFile in:\n%s", str)
	}

	// Verify old values are gone
	if strings.Contains(str, "2.2.2.2") || strings.Contains(str, "olduser") || strings.Contains(str, "2200") {
		t.Errorf("expected old values to be replaced in:\n%s", str)
	}

	// Verify preserved comments and unknown directives
	if !strings.Contains(str, "# Custom comment before host2") {
		t.Errorf("expected outer comment to be preserved")
	}
	if !strings.Contains(str, "# Inline comment inside host2") {
		t.Errorf("expected inline comment inside host2 to be preserved")
	}
	if !strings.Contains(str, "ForwardAgent yes") {
		t.Errorf("expected ForwardAgent yes to be preserved")
	}
	if !strings.Contains(str, "LocalForward 8080 127.0.0.1:80") {
		t.Errorf("expected LocalForward to be preserved")
	}
}

func TestUpdateHostEntry_RenameAndDropDirective(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config")

	initial := `Host oldalias
    HostName 10.0.0.1
    User admin
    Port 2222
    IdentityFile ~/.ssh/id_rsa
`
	if err := os.WriteFile(configPath, []byte(initial), 0600); err != nil {
		t.Fatalf("write initial config: %v", err)
	}

	// Rename oldalias -> newalias and drop Port and IdentityFile (empty in cfg)
	cfg := sshconfig.HostConfig{
		Alias:    "newalias",
		HostName: "10.0.0.2",
		User:     "root",
	}

	err := sshconfig.UpdateHostEntry(configPath, "oldalias", cfg)
	if err != nil {
		t.Fatalf("UpdateHostEntry failed: %v", err)
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read updated config: %v", err)
	}
	str := string(content)

	if strings.Contains(str, "Host oldalias") {
		t.Errorf("expected 'Host oldalias' to be replaced by 'Host newalias' in:\n%s", str)
	}
	if !strings.Contains(str, "Host newalias") {
		t.Errorf("expected 'Host newalias' in:\n%s", str)
	}
	if strings.Contains(str, "Port") || strings.Contains(str, "IdentityFile") {
		t.Errorf("expected Port and IdentityFile to be dropped when empty in cfg in:\n%s", str)
	}
}

func TestUpdateHostEntry_NotFound(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config")
	_ = os.WriteFile(configPath, []byte("Host host1\n    HostName 1.1.1.1\n"), 0600)

	cfg := sshconfig.HostConfig{
		Alias:    "nonexistent",
		HostName: "9.9.9.9",
	}
	err := sshconfig.UpdateHostEntry(configPath, "nonexistent", cfg)
	if err == nil {
		t.Fatalf("expected error for nonexistent host, got nil")
	}
}




func TestWriteKeyToFile_RoundTrip(t *testing.T) {
	privPEM, pubAuth, _, err := sshconfig.GenerateKeyPair("ed25519")
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	dir := filepath.Join(t.TempDir(), "nested", ".ssh")
	keyPath := filepath.Join(dir, "id_ed25519_exported")

	if err := sshconfig.WriteKeyToFile(keyPath, []byte(privPEM)); err != nil {
		t.Fatalf("WriteKeyToFile: %v", err)
	}

	// Private key bytes land on disk verbatim, 0600.
	gotPriv, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("read private key: %v", err)
	}
	if string(gotPriv) != privPEM {
		t.Error("private key bytes on disk differ from supplied PEM")
	}
	if perm := filePerm(t, keyPath); perm != 0600 {
		t.Errorf("private key perm = %o, want 0600", perm)
	}

	// Public key is derived from the private key, and is the authorized-key form.
	gotPub, err := os.ReadFile(keyPath + ".pub")
	if err != nil {
		t.Fatalf("read public key: %v", err)
	}
	if string(gotPub) != pubAuth {
		t.Errorf("public key = %q, want %q", gotPub, pubAuth)
	}
	if perm := filePerm(t, keyPath+".pub"); perm != 0644 {
		t.Errorf("public key perm = %o, want 0644", perm)
	}

	// The private key must round-trip through the parser ssh uses.
	if _, err := ssh.ParseRawPrivateKey(gotPriv); err != nil {
		t.Errorf("written private key does not parse: %v", err)
	}
}

func TestWriteKeyToFile_RejectsGarbage(t *testing.T) {
	keyPath := filepath.Join(t.TempDir(), "id_bad")
	if err := sshconfig.WriteKeyToFile(keyPath, []byte("not a key")); err == nil {
		t.Fatal("expected error for unparsable key material")
	}
	if _, err := os.Stat(keyPath); !os.IsNotExist(err) {
		t.Error("unparsable key must not be written to disk")
	}
}

func filePerm(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return info.Mode().Perm()
}

func TestWriteKeyToFile_RefusesToOverwrite(t *testing.T) {
	privPEM, _, _, err := sshconfig.GenerateKeyPair("ed25519")
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "id_ed25519_dup")

	if err := sshconfig.WriteKeyToFile(keyPath, []byte(privPEM)); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if err := sshconfig.WriteKeyToFile(keyPath, []byte(privPEM)); err == nil {
		t.Fatal("expected refusal to overwrite an existing key file")
	}

	// A pre-planted .pub alone (the private path free) is also a collision, and
	// the private key written for it must be rolled back.
	keyPath2 := filepath.Join(dir, "id_ed25519_pubonly")
	if err := os.WriteFile(keyPath2+".pub", []byte("planted"), 0644); err != nil {
		t.Fatalf("plant .pub: %v", err)
	}
	if err := sshconfig.WriteKeyToFile(keyPath2, []byte(privPEM)); err == nil {
		t.Fatal("expected refusal when the .pub already exists")
	}
	if _, err := os.Stat(keyPath2); !os.IsNotExist(err) {
		t.Error("private key must be rolled back when the .pub exists")
	}
	if got, _ := os.ReadFile(keyPath2 + ".pub"); string(got) != "planted" {
		t.Errorf("planted .pub was modified: %q", got)
	}
}

func TestWriteKeyToFile_RefusesSymlinkTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on Windows")
	}
	privPEM, _, _, err := sshconfig.GenerateKeyPair("ed25519")
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	dir := t.TempDir()
	victim := filepath.Join(dir, "victim")
	if err := os.WriteFile(victim, []byte("untouched"), 0600); err != nil {
		t.Fatalf("seed victim: %v", err)
	}
	keyPath := filepath.Join(dir, "id_ed25519_link")
	if err := os.Symlink(victim, keyPath); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	if err := sshconfig.WriteKeyToFile(keyPath, []byte(privPEM)); err == nil {
		t.Fatal("expected refusal for a symlinked key path")
	}
	if got, _ := os.ReadFile(victim); string(got) != "untouched" {
		t.Errorf("symlink target was overwritten: %q", got)
	}
}
