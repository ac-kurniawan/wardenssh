package tviewui_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/ac-kurniawan/wardenssh/internal/config"
	"github.com/ac-kurniawan/wardenssh/internal/hosts"
	"github.com/ac-kurniawan/wardenssh/internal/sshconfig"
	"github.com/ac-kurniawan/wardenssh/internal/tviewui"
	"github.com/ac-kurniawan/wardenssh/internal/vault"
	"github.com/ac-kurniawan/wardenssh/internal/vaultadapter"
	"github.com/ac-kurniawan/wardenssh/internal/vaultclient"
)

// vaultCopyFixture builds an app with one vault-sourced host entry whose vault
// item carries a real (parseable) private key in plaintext.
func vaultCopyFixture(t *testing.T) (*tviewui.App, hosts.Entry, string, string) {
	t.Helper()
	privPEM, _, _, err := sshconfig.GenerateKeyPair("ed25519")
	if err != nil {
		t.Fatalf("generate keypair: %v", err)
	}
	entry := hosts.Entry{
		Alias:    "vaultbox",
		HostName: "10.9.9.9",
		User:     "deploy",
		Port:     "22",
		Source:   "vw:work",
		AuthKind: "key",
	}
	src := vault.NewFakeSource("vw:work", []vault.Item{{
		Name:          "vaultbox",
		HostName:      "10.9.9.9",
		User:          "deploy",
		Port:          "22",
		EncPrivateKey: privPEM,
	}})
	app := tviewui.New(hosts.NewList([]hosts.Entry{entry}), tviewui.Deps{
		VaultCli: vault.NewFakeClient(src),
	}, nil)

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config")
	app.SetSSHConfigPathForTest(configPath)
	return app, entry, configPath, dir
}

func TestApp_HandleCopyVaultToLocal_WritesKeyAndHostBlock(t *testing.T) {
	app, entry, configPath, dir := vaultCopyFixture(t)

	if err := app.HandleCopyVaultToLocal(entry); err != nil {
		t.Fatalf("HandleCopyVaultToLocal: %v", err)
	}

	keyPath := filepath.Join(dir, "id_ed25519_vaultbox")
	info, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("expected private key at %s: %v", keyPath, err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("private key perm = %o, want 0600", perm)
	}
	if _, err := os.Stat(keyPath + ".pub"); err != nil {
		t.Errorf("expected public key at %s.pub: %v", keyPath, err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	cfgStr := string(data)
	for _, want := range []string{"Host vaultbox", "HostName 10.9.9.9", "User deploy", "IdentityFile " + keyPath} {
		if !strings.Contains(cfgStr, want) {
			t.Errorf("config missing %q:\n%s", want, cfgStr)
		}
	}

	var found *hosts.Entry
	for _, e := range app.HostList().All() {
		if e.Alias == "vaultbox" && e.Source == "file" {
			found = &e
			break
		}
	}
	if found == nil {
		t.Fatalf("expected a file-sourced vaultbox entry in the host list: %+v", app.HostList().All())
	}
	if found.IdentityFile != keyPath {
		t.Errorf("merged IdentityFile = %q, want %q", found.IdentityFile, keyPath)
	}
}

func TestApp_HandleCopyVaultToLocal_RefusesExistingKeyFile(t *testing.T) {
	app, entry, configPath, dir := vaultCopyFixture(t)

	keyPath := filepath.Join(dir, "id_ed25519_vaultbox")
	if err := os.WriteFile(keyPath, []byte("pre-existing"), 0600); err != nil {
		t.Fatalf("seed key file: %v", err)
	}

	if err := app.HandleCopyVaultToLocal(entry); err == nil {
		t.Fatal("expected collision error, got nil")
	}
	if got, _ := os.ReadFile(keyPath); string(got) != "pre-existing" {
		t.Errorf("existing key file was overwritten: %q", got)
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Error("collision must not write a host block")
	}
}

func TestApp_HandleCopyVaultToLocal_RefusesPasswordEntry(t *testing.T) {
	app, entry, _, _ := vaultCopyFixture(t)
	entry.AuthKind = "password"

	if err := app.HandleCopyVaultToLocal(entry); err == nil {
		t.Fatal("expected refusal for password entry, got nil")
	}
}

func TestApp_HandleCopyVaultToLocal_UnknownItem(t *testing.T) {
	app, entry, _, _ := vaultCopyFixture(t)
	entry.Alias = "ghost"

	if err := app.HandleCopyVaultToLocal(entry); err == nil {
		t.Fatal("expected error for missing vault item, got nil")
	}
}

// TestApp_HandleCopyLocalToVault_CopyKeep creates a vault SSH-Key cipher from a
// local key file and leaves the local key + config untouched.
func TestApp_HandleCopyLocalToVault_CopyKeep(t *testing.T) {
	privPEM, _, _, err := sshconfig.GenerateKeyPair("ed25519")
	if err != nil {
		t.Fatalf("generate keypair: %v", err)
	}
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "id_ed25519_localbox")
	if err := sshconfig.WriteKeyToFile(keyPath, []byte(privPEM)); err != nil {
		t.Fatalf("write local key: %v", err)
	}

	symKey := bytes.Repeat([]byte{0x03}, 64)
	sess := &vaultclient.Session{
		AccessToken: "copy-token",
		SymEnc:      symKey[:32],
		SymMac:      symKey[32:],
	}

	var postedCipher vaultclient.Cipher
	mux := http.NewServeMux()
	mux.HandleFunc("/api/ciphers", func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&postedCipher); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		postedCipher.ID = "cipher-copy-1"
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(postedCipher)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	cf := config.CustomFields{Host: "host", User: "user", Port: "port", ProxyJump: "proxyjump", Type: "type"}
	src := vaultadapter.NewSource("vw:work", sess, nil, cf)
	vc := vaultadapter.NewClient(src)

	entry := hosts.Entry{
		Alias:        "localbox",
		HostName:     "10.7.7.7",
		User:         "ops",
		Port:         "2222",
		Source:       "file",
		IdentityFile: keyPath,
		AuthKind:     "key",
	}
	app := tviewui.New(hosts.NewList([]hosts.Entry{entry}), tviewui.Deps{
		VaultCli:     vc,
		CustomFields: cf,
	}, []config.Vault{{Name: "work", Server: srv.URL, Email: "work@example.com"}})

	if err := app.HandleCopyLocalToVault(entry, "vw:work"); err != nil {
		t.Fatalf("HandleCopyLocalToVault: %v", err)
	}

	if postedCipher.Type != 5 {
		t.Errorf("posted cipher Type = %d, want 5 (SSH-Key)", postedCipher.Type)
	}
	if postedCipher.SshKey == nil {
		t.Fatal("expected non-nil SshKey")
	}
	gotPriv, err := sess.DecryptField(postedCipher.SshKey.PrivateKey)
	if err != nil || string(gotPriv) != privPEM {
		t.Errorf("vault private key != local private key (err=%v)", err)
	}
	fp, err := sess.DecryptField(postedCipher.SshKey.KeyFingerprint)
	if err != nil || !strings.HasPrefix(string(fp), "SHA256:") {
		t.Errorf("keyFingerprint = %q, err=%v, want SHA256:...", fp, err)
	}

	custom := make(map[string]string)
	for _, f := range postedCipher.Fields {
		k, _ := sess.DecryptField(f.Name)
		v, _ := sess.DecryptField(f.Value)
		custom[string(k)] = string(v)
	}
	if custom["host"] != "10.7.7.7" || custom["user"] != "ops" || custom["port"] != "2222" {
		t.Errorf("custom fields = %v, want host/user/port copied", custom)
	}
	if !strings.EqualFold(custom["type"], "SSH") {
		t.Errorf("type field = %q, want SSH", custom["type"])
	}

	// Copy-keep: local key file survives untouched.
	if got, err := os.ReadFile(keyPath); err != nil || string(got) != privPEM {
		t.Errorf("local key file changed after copy (err=%v)", err)
	}

	var found *hosts.Entry
	for _, e := range app.HostList().All() {
		if e.Alias == "localbox" && e.Source == "vw:work" {
			found = &e
			break
		}
	}
	if found == nil {
		t.Fatalf("expected merged vault entry: %+v", app.HostList().All())
	}
}

func TestApp_HandleCopyLocalToVault_RefusesMissingKey(t *testing.T) {
	entry := hosts.Entry{Alias: "nokey", Source: "file", HostName: "10.0.0.1", AuthKind: "key"}
	app := tviewui.New(hosts.NewList([]hosts.Entry{entry}), tviewui.Deps{}, nil)

	if err := app.HandleCopyLocalToVault(entry, "vw:work"); err == nil {
		t.Fatal("expected error for entry without IdentityFile, got nil")
	}
}

func TestApp_HandleCopyLocalToVault_RefusesUnlockedMissingVault(t *testing.T) {
	privPEM, _, _, err := sshconfig.GenerateKeyPair("ed25519")
	if err != nil {
		t.Fatalf("generate keypair: %v", err)
	}
	keyPath := filepath.Join(t.TempDir(), "id_ed25519_x")
	if err := sshconfig.WriteKeyToFile(keyPath, []byte(privPEM)); err != nil {
		t.Fatalf("write key: %v", err)
	}
	entry := hosts.Entry{Alias: "x", Source: "file", IdentityFile: keyPath, AuthKind: "key"}
	app := tviewui.New(hosts.NewList([]hosts.Entry{entry}), tviewui.Deps{}, nil)

	if err := app.HandleCopyLocalToVault(entry, "vw:work"); err == nil {
		t.Fatal("expected error when no vault client is configured, got nil")
	}
}

// unlockedVaultDeps builds a vault client with one unlocked source (no HTTP).
func unlockedVaultDeps() tviewui.Deps {
	symKey := bytes.Repeat([]byte{0x04}, 64)
	sess := &vaultclient.Session{
		AccessToken: "route-token",
		SymEnc:      symKey[:32],
		SymMac:      symKey[32:],
	}
	src := vaultadapter.NewSource("vw:work", sess, nil, config.CustomFields{})
	return tviewui.Deps{VaultCli: vaultadapter.NewClient(src)}
}

func TestApp_CtrlT_VaultEntryOffersVaultToLocal(t *testing.T) {
	entry := hosts.Entry{Alias: "vaultbox", HostName: "10.9.9.9", Source: "vw:work", AuthKind: "key"}
	app := tviewui.New(hosts.NewList([]hosts.Entry{entry}), unlockedVaultDeps(), nil)
	app.SetSSHConfigPathForTest(filepath.Join(t.TempDir(), "config"))
	app.HostPane().Refresh()

	app.HandleGlobalKey(tcell.NewEventKey(tcell.KeyCtrlT, 0, tcell.ModNone))

	if !app.InCopyModal() {
		t.Fatal("Ctrl+T on a vault entry must open the copy modal")
	}
	cm := app.CopyModal()
	if got := cm.CopyDirection(); got != "vault → local" {
		t.Errorf("direction = %q, want 'vault → local'", got)
	}
	if got := cm.CopyTargetAlias(); got != "vaultbox" {
		t.Errorf("alias = %q, want vaultbox", got)
	}
	if !strings.Contains(cm.Text(), "written to disk") {
		t.Errorf("vault→local modal must warn about disk write: %q", cm.Text())
	}
}

func TestApp_CtrlT_FileEntryOffersLocalToVault(t *testing.T) {
	entry := hosts.Entry{Alias: "localbox", HostName: "10.7.7.7", Source: "file", AuthKind: "key"}
	app := tviewui.New(hosts.NewList([]hosts.Entry{entry}), unlockedVaultDeps(), nil)
	app.HostPane().Refresh()

	app.HandleGlobalKey(tcell.NewEventKey(tcell.KeyCtrlT, 0, tcell.ModNone))

	if !app.InCopyModal() {
		t.Fatal("Ctrl+T on a file entry must open the copy modal when a vault is unlocked")
	}
	cm := app.CopyModal()
	if got := cm.CopyDirection(); got != "local → vault" {
		t.Errorf("direction = %q, want 'local → vault'", got)
	}
	if !strings.Contains(cm.Text(), "vw:work") {
		t.Errorf("local→vault modal must name the target vault: %q", cm.Text())
	}
}

func TestApp_CtrlT_FileEntryWithoutVaultIsNoOp(t *testing.T) {
	entry := hosts.Entry{Alias: "localbox", HostName: "10.7.7.7", Source: "file", AuthKind: "key"}
	app := tviewui.New(hosts.NewList([]hosts.Entry{entry}), tviewui.Deps{}, nil)
	app.HostPane().Refresh()

	app.HandleGlobalKey(tcell.NewEventKey(tcell.KeyCtrlT, 0, tcell.ModNone))

	if app.InCopyModal() {
		t.Error("Ctrl+T with no unlocked vault must not open the copy modal")
	}
}

func TestApp_CtrlT_NoSelectionIsNoOp(t *testing.T) {
	app := tviewui.New(hosts.NewList(nil), tviewui.Deps{}, nil)
	app.HostPane().Refresh()

	app.HandleGlobalKey(tcell.NewEventKey(tcell.KeyCtrlT, 0, tcell.ModNone))

	if app.InCopyModal() {
		t.Error("Ctrl+T with no selection must not open the copy modal")
	}
}

func TestApp_CopyModalConfirmCopiesVaultEntry(t *testing.T) {
	app, _, _, dir := vaultCopyFixture(t)
	app.HostPane().Refresh()

	app.HandleGlobalKey(tcell.NewEventKey(tcell.KeyCtrlT, 0, tcell.ModNone))
	if !app.InCopyModal() {
		t.Fatal("precondition: Ctrl+T must open the copy modal")
	}
	app.ConfirmCopy()

	if app.InCopyModal() {
		t.Error("confirming the copy must close the modal")
	}
	if _, err := os.Stat(filepath.Join(dir, "id_ed25519_vaultbox")); err != nil {
		t.Errorf("expected exported key file: %v", err)
	}
}

// A vault item name is attacker-influenced data (it is whatever the vault
// holds): it must never steer the exported key file outside ~/.ssh.
func TestApp_HandleCopyVaultToLocal_AliasCannotEscapeSSHDir(t *testing.T) {
	privPEM, _, _, err := sshconfig.GenerateKeyPair("ed25519")
	if err != nil {
		t.Fatalf("generate keypair: %v", err)
	}
	alias := "../../escape"
	src := vault.NewFakeSource("vw:work", []vault.Item{{
		Name:          alias,
		HostName:      "10.9.9.9",
		EncPrivateKey: privPEM,
	}})
	entry := hosts.Entry{Alias: alias, HostName: "10.9.9.9", Source: "vw:work", AuthKind: "key"}
	app := tviewui.New(hosts.NewList([]hosts.Entry{entry}), tviewui.Deps{
		VaultCli: vault.NewFakeClient(src),
	}, nil)

	dir := t.TempDir()
	sshDir := filepath.Join(dir, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	configPath := filepath.Join(sshDir, "config")
	app.SetSSHConfigPathForTest(configPath)

	if err := app.HandleCopyVaultToLocal(entry); err != nil {
		t.Fatalf("HandleCopyVaultToLocal: %v", err)
	}

	// Nothing may be written outside the ssh dir.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != ".ssh" {
		t.Errorf("copy wrote outside the ssh dir: %v", entries)
	}

	var found *hosts.Entry
	for _, e := range app.HostList().All() {
		if e.Source == "file" {
			found = &e
			break
		}
	}
	if found == nil {
		t.Fatal("expected merged file entry")
	}
	if !strings.HasPrefix(found.IdentityFile, sshDir+string(os.PathSeparator)) {
		t.Errorf("IdentityFile %q escapes %q", found.IdentityFile, sshDir)
	}
}

// A deep ../ chain in a vault item name must not escape ~/.ssh (a single
// "../" is already neutralized by the id_<algo>_ prefix; several are not).
func TestApp_HandleCopyVaultToLocal_DeepTraversalStaysInSSHDir(t *testing.T) {
	privPEM, _, _, err := sshconfig.GenerateKeyPair("ed25519")
	if err != nil {
		t.Fatalf("generate keypair: %v", err)
	}
	alias := "../../../../../../tmp/wardenssh-pwn"
	src := vault.NewFakeSource("vw:work", []vault.Item{{
		Name:          alias,
		HostName:      "10.9.9.9",
		EncPrivateKey: privPEM,
	}})
	entry := hosts.Entry{Alias: alias, HostName: "10.9.9.9", Source: "vw:work", AuthKind: "key"}
	app := tviewui.New(hosts.NewList([]hosts.Entry{entry}), tviewui.Deps{
		VaultCli: vault.NewFakeClient(src),
	}, nil)

	sshDir := filepath.Join(t.TempDir(), ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	app.SetSSHConfigPathForTest(filepath.Join(sshDir, "config"))

	if err := app.HandleCopyVaultToLocal(entry); err != nil {
		t.Fatalf("HandleCopyVaultToLocal: %v", err)
	}

	for _, e := range app.HostList().All() {
		if e.Source != "file" {
			continue
		}
		if !strings.HasPrefix(e.IdentityFile, sshDir+string(os.PathSeparator)) {
			t.Fatalf("IdentityFile %q escaped %q", e.IdentityFile, sshDir)
		}
		if _, err := os.Stat(e.IdentityFile); err != nil {
			t.Errorf("exported key missing at %s: %v", e.IdentityFile, err)
		}
	}
}

// A newline in a vault item name would inject extra directives into the
// `Host` line: the export refuses instead.
func TestApp_HandleCopyVaultToLocal_RefusesInjectingAlias(t *testing.T) {
	privPEM, _, _, err := sshconfig.GenerateKeyPair("ed25519")
	if err != nil {
		t.Fatalf("generate keypair: %v", err)
	}
	alias := "box\n    ProxyCommand sh -c id"
	src := vault.NewFakeSource("vw:work", []vault.Item{{
		Name:          alias,
		HostName:      "10.9.9.9",
		EncPrivateKey: privPEM,
	}})
	entry := hosts.Entry{Alias: alias, HostName: "10.9.9.9", Source: "vw:work", AuthKind: "key"}
	app := tviewui.New(hosts.NewList([]hosts.Entry{entry}), tviewui.Deps{
		VaultCli: vault.NewFakeClient(src),
	}, nil)

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config")
	app.SetSSHConfigPathForTest(configPath)

	if err := app.HandleCopyVaultToLocal(entry); err == nil {
		t.Fatal("expected refusal for alias with control characters, got nil")
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Error("refused export must not write a host block")
	}
}

// HostName/User/Port/ProxyJump are vault-supplied too: a newline in any of them
// would inject directives into the appended Host block.
func TestApp_HandleCopyVaultToLocal_RefusesInjectingFields(t *testing.T) {
	privPEM, _, _, err := sshconfig.GenerateKeyPair("ed25519")
	if err != nil {
		t.Fatalf("generate keypair: %v", err)
	}
	src := vault.NewFakeSource("vw:work", []vault.Item{{
		Name:          "box",
		HostName:      "10.9.9.9\n    ProxyCommand sh -c id",
		EncPrivateKey: privPEM,
	}})
	entry := hosts.Entry{
		Alias:    "box",
		HostName: "10.9.9.9\n    ProxyCommand sh -c id",
		Source:   "vw:work",
		AuthKind: "key",
	}
	app := tviewui.New(hosts.NewList([]hosts.Entry{entry}), tviewui.Deps{
		VaultCli: vault.NewFakeClient(src),
	}, nil)

	configPath := filepath.Join(t.TempDir(), "config")
	app.SetSSHConfigPathForTest(configPath)

	if err := app.HandleCopyVaultToLocal(entry); err == nil {
		t.Fatal("expected refusal for a host field with control characters, got nil")
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Error("refused export must not write a host block")
	}
}
