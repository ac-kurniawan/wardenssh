package tviewui_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/ac-kurniawan/wardenssh/internal/config"
	"github.com/ac-kurniawan/wardenssh/internal/hosts"
	"github.com/ac-kurniawan/wardenssh/internal/sshconfig"
	"github.com/ac-kurniawan/wardenssh/internal/tviewui"
	"github.com/ac-kurniawan/wardenssh/internal/vault"
	"github.com/ac-kurniawan/wardenssh/internal/vaultadapter"
	"github.com/ac-kurniawan/wardenssh/internal/vaultclient"
)

func TestAppNewWithoutVaults(t *testing.T) {
	hl := sampleHostList()
	app := tviewui.New(hl, tviewui.Deps{}, nil)
	if app == nil {
		t.Fatal("expected non-nil App")
	}
	if app.InSetup() {
		t.Fatal("expected to not be in setup mode without vaults")
	}
}

func TestAppNewWithVaultsStartsInSetup(t *testing.T) {
	hl := sampleHostList()
	app := tviewui.New(hl, tviewui.Deps{}, sampleVaults())
	if app == nil {
		t.Fatal("expected non-nil App")
	}
	if !app.InSetup() {
		t.Fatal("expected to be in setup mode with vaults configured")
	}
}

func TestAppSetupSkipTransitionsToList(t *testing.T) {
	hl := sampleHostList()
	app := tviewui.New(hl, tviewui.Deps{}, sampleVaults())
	app.SkipSetup()
	if app.InSetup() {
		t.Fatal("expected to leave setup after SkipSetup")
	}
}

func TestAppQuitWithNoLiveSessions(t *testing.T) {
	hl := sampleHostList()
	app := tviewui.New(hl, tviewui.Deps{}, nil)
	quit := app.RequestQuit()
	if quit {
		t.Error("expected RequestQuit to return false (always show confirmation modal)")
	}
	if !app.InQuitModal() {
		t.Fatal("expected quit confirmation modal even with no live sessions")
	}
}

// TestAppEnablesPaste: pasting multi-line text into the embedded terminal
// (e.g. formatted JSON/YAML) must arrive as one bracketed-paste block, not as
// per-character key events. That requires the tview app to enable paste events
// (which makes the host terminal wrap pastes in \x1b[200~...\x1b[201~ and makes
// tview deliver them as a single string to the focused terminal view).
func TestAppEnablesPaste(t *testing.T) {
	hl := sampleHostList()
	app := tviewui.New(hl, tviewui.Deps{}, nil)
	if !app.PasteEnabled() {
		t.Fatal("expected the app to enable bracketed paste for the terminal pane")
	}
}

// TestAppEscAtHomeShowsQuitModal: pressing Escape in the host list (home)
// must open the quit confirmation modal, not quit directly.
func TestAppEscAtHomeShowsQuitModal(t *testing.T) {
	hl := sampleHostList()
	app := tviewui.New(hl, tviewui.Deps{}, nil)
	if app.InQuitModal() {
		t.Fatal("expected no quit modal before Escape")
	}
	app.HandleGlobalKey(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))
	if !app.InQuitModal() {
		t.Fatal("expected quit confirmation modal after Escape at home")
	}
}

// TestAppQAtHomeShowsQuitModal: pressing 'q' with no live sessions must open
// the quit confirmation modal (not quit directly) per the user's requirement.
func TestAppQAtHomeShowsQuitModal(t *testing.T) {
	hl := sampleHostList()
	app := tviewui.New(hl, tviewui.Deps{}, nil)
	app.HandleGlobalKey(tcell.NewEventKey(tcell.KeyRune, 'q', tcell.ModNone))
	if !app.InQuitModal() {
		t.Fatal("expected quit confirmation modal after 'q' at home")
	}
}

// TestAppQuitModalCancelReturnsToList: 'c' in the quit modal returns to the
// host list and dismisses the modal.
func TestAppQuitModalCancelReturnsToList(t *testing.T) {
	hl := sampleHostList()
	app := tviewui.New(hl, tviewui.Deps{}, nil)
	app.RequestQuit()
	if !app.InQuitModal() {
		t.Fatal("expected quit modal before cancel")
	}
	app.CancelQuit()
	if app.InQuitModal() {
		t.Fatal("expected quit modal dismissed after cancel")
	}
}

// TestAppQuitModalKeybindingsKillsAll: 'k' in the quit modal triggers kill-all.
func TestAppQuitModalKeybindingsKillsAll(t *testing.T) {
	hl := sampleHostList()
	hl.MarkLive("prod-db-01", "file")
	app := tviewui.New(hl, tviewui.Deps{}, nil)
	app.RequestQuit()
	if !app.InQuitModal() {
		t.Fatal("expected quit modal")
	}
	app.KillAllQuit()
	if app.InQuitModal() {
		t.Fatal("expected quit modal dismissed after kill-all")
	}
	if liveCount := liveCountOf(app); liveCount != 0 {
		t.Errorf("expected no live sessions after kill-all, got %d", liveCount)
	}
}

func TestAppQuitWithLiveSessionsShowsModal(t *testing.T) {
	hl := sampleHostList()
	hl.MarkLive("prod-db-01", "file")
	app := tviewui.New(hl, tviewui.Deps{}, nil)
	quit := app.RequestQuit()
	if quit {
		t.Error("expected RequestQuit to return false (show modal) with live sessions")
	}
	if !app.InQuitModal() {
		t.Fatal("expected to be in quit modal")
	}
}

func TestAppTriggerSyncRunsSync(t *testing.T) {
	hl := sampleHostList()
	fc := vault.NewFakeClient()
	app := tviewui.New(hl, tviewui.Deps{VaultCli: fc}, nil)

	<-app.TriggerSync()

	title := app.HostPane().Title()
	if !strings.Contains(title, "Synced") {
		t.Errorf("expected title to contain 'Synced', got: %s", title)
	}
}

func TestAppStartBackgroundSync(t *testing.T) {
	hl := sampleHostList()
	fc := vault.NewFakeClient()
	app := tviewui.New(hl, tviewui.Deps{VaultCli: fc}, nil)

	app.StartBackgroundSync(10 * time.Millisecond)
	time.Sleep(50 * time.Millisecond)
	app.StopBackgroundSync()

	title := app.HostPane().Title()
	if !strings.Contains(title, "Synced") {
		t.Errorf("expected title to contain 'Synced' after background sync, got: %s", title)
	}
}

type failingVaultClient struct{}

func (f *failingVaultClient) Sources() []vault.Source { return nil }
func (f *failingVaultClient) Sync() error             { return fmt.Errorf("network connection failed") }

func TestAppTriggerSyncOfflineStatusOnSyncError(t *testing.T) {
	hl := sampleHostList()
	fc := &failingVaultClient{}
	app := tviewui.New(hl, tviewui.Deps{VaultCli: fc}, nil)

	<-app.TriggerSync()

	title := app.HostPane().Title()
	if !strings.Contains(title, "Sync failed (offline)") {
		t.Errorf("expected title to contain 'Sync failed (offline)', got: %s", title)
	}
}

var _ = config.CustomFields{}
var _ = vaultadapter.Client{}

// TestAppConnectPasswordHostWithoutVaultClientDoesNotSpawn: a password host
// with no vault client must abort before spawning a session or marking live.
func TestAppConnectPasswordHostWithoutVaultClientDoesNotSpawn(t *testing.T) {
	hl := hosts.NewList([]hosts.Entry{
		{Alias: "prod-db", HostName: "10.0.0.9", Source: "vw:personal", AuthKind: "password"},
	})
	app := tviewui.New(hl, tviewui.Deps{}, nil)
	app.HandleConnectForTest(hosts.Entry{Alias: "prod-db", HostName: "10.0.0.9", Source: "vw:personal", AuthKind: "password"})
	if app.TerminalPane().IsRunning() {
		t.Error("expected no session spawned without a vault client")
	}
	if isLive(hl, "prod-db", "vw:personal") {
		t.Error("expected host not marked live when connect aborted")
	}
}

func liveCountOf(app *tviewui.App) int {
	n := 0
	for _, e := range app.HostList().All() {
		if e.Live {
			n++
		}
	}
	return n
}

func TestAppSetupOnCompleteFromGoroutine(t *testing.T) {
	tviewui.SetKeyringGetRefreshTokenForTest(func(vName string) (string, error) {
		if vName == "myvault" {
			return "valid-ref-token", nil
		}
		return "", fmt.Errorf("no token")
	})
	defer tviewui.ResetKeyringGetRefreshTokenForTest()

	mux := http.NewServeMux()
	mux.HandleFunc("/identity/connect/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]string{
			"access_token":  "mock-at",
			"refresh_token": "valid-ref-token",
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/ciphers", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	vaults := []config.Vault{
		{Name: "myvault", Server: srv.URL, Email: "user@example.com"},
	}
	hl := sampleHostList()
	app := tviewui.New(hl, tviewui.Deps{}, vaults)

	time.Sleep(200 * time.Millisecond)

	if app == nil {
		t.Fatal("expected non-nil app")
	}
}

func setupTestAppWithConfigPath(configPath string) *tviewui.App {
	hl := hosts.NewList(nil)
	app := tviewui.New(hl, tviewui.Deps{}, nil)
	app.SetSSHConfigPathForTest(configPath)
	return app
}

func TestApp_CreateConnection_FileTarget_KeyAuth(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config")

	app := setupTestAppWithConfigPath(configPath)

	params := tviewui.CreateParams{
		Alias:    "new-server",
		Target:   "~/.ssh/config",
		HostName: "10.0.0.5",
		User:     "root",
		Port:     "22",
		AuthKind: "key",
		KeyAlgo:  "ed25519",
	}

	err := app.HandleCreateConnection(params)
	if err != nil {
		t.Fatalf("HandleCreateConnection failed: %v", err)
	}

	// Verify host list updated
	entries := app.HostList().All()
	var found *hosts.Entry
	for _, e := range entries {
		if e.Alias == "new-server" && e.HostName == "10.0.0.5" {
			found = &e
			break
		}
	}
	if found == nil {
		t.Fatalf("new-server host not found in host list after creation: %+v", entries)
	}
	if found.Source != "file" {
		t.Errorf("Source = %q, want 'file'", found.Source)
	}
	if found.AuthKind != "key" {
		t.Errorf("AuthKind = %q, want 'key'", found.AuthKind)
	}
	if found.IdentityFile == "" {
		t.Errorf("IdentityFile should be set for key auth")
	}

	// Verify key file was generated
	if _, err := os.Stat(found.IdentityFile); err != nil {
		t.Errorf("expected identity file at %s: %v", found.IdentityFile, err)
	}
	if _, err := os.Stat(found.IdentityFile + ".pub"); err != nil {
		t.Errorf("expected public key file at %s.pub: %v", found.IdentityFile, err)
	}

	// Verify ssh config content
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config file: %v", err)
	}
	cfgStr := string(data)
	if !strings.Contains(cfgStr, "Host new-server") || !strings.Contains(cfgStr, "HostName 10.0.0.5") {
		t.Errorf("unexpected config file content:\n%s", cfgStr)
	}
}

func TestApp_CreateConnection_FileTarget_PasswordAuth(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config")

	app := setupTestAppWithConfigPath(configPath)

	params := tviewui.CreateParams{
		Alias:    "db-node",
		Target:   "file",
		HostName: "192.168.1.50",
		User:     "postgres",
		Port:     "5432",
		AuthKind: "password",
		Password: "secretpassword",
	}

	err := app.HandleCreateConnection(params)
	if err != nil {
		t.Fatalf("HandleCreateConnection failed: %v", err)
	}

	entries := app.HostList().All()
	var found *hosts.Entry
	for _, e := range entries {
		if e.Alias == "db-node" && e.HostName == "192.168.1.50" {
			found = &e
			break
		}
	}
	if found == nil {
		t.Fatalf("db-node host not found in host list after creation: %+v", entries)
	}
	if found.AuthKind != "password" {
		t.Errorf("AuthKind = %q, want 'password'", found.AuthKind)
	}
	if found.IdentityFile != "" {
		t.Errorf("IdentityFile should be empty for password auth, got %q", found.IdentityFile)
	}

	// Verify config file
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config file: %v", err)
	}
	cfgStr := string(data)
	if !strings.Contains(cfgStr, "Host db-node") || !strings.Contains(cfgStr, "Port 5432") {
		t.Errorf("unexpected config file content:\n%s", cfgStr)
	}
}

func TestApp_CreateConnection_VaultTarget_PasswordAuth(t *testing.T) {
	symKey := bytes.Repeat([]byte{0x01}, 64)
	sess := &vaultclient.Session{
		AccessToken: "test-token",
		SymEnc:      symKey[:32],
		SymMac:      symKey[32:],
	}

	var postedCipher vaultclient.Cipher
	var authHeader string

	mux := http.NewServeMux()
	mux.HandleFunc("/api/ciphers", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "bad method", http.StatusMethodNotAllowed)
			return
		}
		authHeader = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&postedCipher); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		postedCipher.ID = "cipher-123"
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(postedCipher)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	cf := config.CustomFields{
		Host:      "host",
		User:      "user",
		Port:      "port",
		ProxyJump: "proxyjump",
		Type:      "type",
	}
	src := vaultadapter.NewSource("vw:personal", sess, nil, cf)
	vc := vaultadapter.NewClient(src)

	vaults := []config.Vault{
		{Name: "personal", Server: srv.URL, Email: "user@example.com"},
	}
	hl := hosts.NewList(nil)
	app := tviewui.New(hl, tviewui.Deps{
		VaultCli:     vc,
		CustomFields: cf,
	}, vaults)

	params := tviewui.CreateParams{
		Alias:     "vw-pass-server",
		Target:    "vw:personal",
		HostName:  "10.0.0.10",
		User:      "admin",
		Port:      "2222",
		ProxyJump: "bastion",
		AuthKind:  "password",
		Password:  "mypassword123",
	}

	err := app.HandleCreateConnection(params)
	if err != nil {
		t.Fatalf("HandleCreateConnection failed: %v", err)
	}

	if authHeader != "Bearer test-token" {
		t.Errorf("Authorization header = %q, want 'Bearer test-token'", authHeader)
	}
	if postedCipher.Type != 1 {
		t.Errorf("posted cipher Type = %d, want 1 (Login)", postedCipher.Type)
	}

	// Verify decrypted name
	nameBytes, err := sess.DecryptField(postedCipher.Name)
	if err != nil || string(nameBytes) != "vw-pass-server" {
		t.Errorf("decrypted cipher name = %q, err=%v", string(nameBytes), err)
	}

	// Verify login fields
	if postedCipher.Login == nil {
		t.Fatalf("expected posted cipher Login to be non-nil")
	}
	userBytes, err := sess.DecryptField(postedCipher.Login.Username)
	if err != nil || string(userBytes) != "admin" {
		t.Errorf("decrypted username = %q, err=%v", string(userBytes), err)
	}
	passBytes, err := sess.DecryptField(postedCipher.Login.Password)
	if err != nil || string(passBytes) != "mypassword123" {
		t.Errorf("decrypted password = %q, err=%v", string(passBytes), err)
	}

	// Verify custom fields
	customMap := make(map[string]string)
	for _, f := range postedCipher.Fields {
		k, _ := sess.DecryptField(f.Name)
		v, _ := sess.DecryptField(f.Value)
		customMap[string(k)] = string(v)
	}
	if customMap["host"] != "10.0.0.10" {
		t.Errorf("custom field 'host' = %q, want '10.0.0.10'", customMap["host"])
	}
	if !strings.EqualFold(customMap["type"], "SSH") {
		t.Errorf("custom field 'type' = %q, want 'SSH'", customMap["type"])
	}
	if customMap["port"] != "2222" {
		t.Errorf("custom field 'port' = %q, want '2222'", customMap["port"])
	}
	if customMap["proxyjump"] != "bastion" {
		t.Errorf("custom field 'proxyjump' = %q, want 'bastion'", customMap["proxyjump"])
	}

	// Verify host list updated
	entries := app.HostList().All()
	var found *hosts.Entry
	for _, e := range entries {
		if e.Alias == "vw-pass-server" && e.HostName == "10.0.0.10" {
			found = &e
			break
		}
	}
	if found == nil {
		t.Fatalf("vw-pass-server host not found in host list: %+v", entries)
	}
	if found.Source != "vw:personal" && found.Source != "personal" {
		t.Errorf("Source = %q, want 'vw:personal' or 'personal'", found.Source)
	}
	if found.AuthKind != "password" {
		t.Errorf("AuthKind = %q, want 'password'", found.AuthKind)
	}
}

func TestApp_CreateConnection_VaultTarget_VWName(t *testing.T) {
	symKey := bytes.Repeat([]byte{0x01}, 64)
	sess := &vaultclient.Session{
		AccessToken: "test-token",
		SymEnc:      symKey[:32],
		SymMac:      symKey[32:],
	}

	var postedCipher vaultclient.Cipher
	mux := http.NewServeMux()
	mux.HandleFunc("/api/ciphers", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&postedCipher)
		postedCipher.ID = "cipher-vw-999"
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(postedCipher)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	cf := config.CustomFields{Host: "host", Type: "type"}
	src := vaultadapter.NewSource("vw", sess, nil, cf)
	vc := vaultadapter.NewClient(src)

	vaults := []config.Vault{
		{Name: "vw", Server: srv.URL, Email: "user@example.com"},
	}
	hl := hosts.NewList(nil)
	app := tviewui.New(hl, tviewui.Deps{
		VaultCli:     vc,
		CustomFields: cf,
	}, vaults)

	params := tviewui.CreateParams{
		Alias:    "vw-server",
		Target:   "vw",
		HostName: "10.0.0.99",
		AuthKind: "password",
		Password: "pass",
	}

	err := app.HandleCreateConnection(params)
	if err != nil {
		t.Fatalf("HandleCreateConnection failed: %v", err)
	}

	entries := app.HostList().All()
	var found *hosts.Entry
	for _, e := range entries {
		if e.Alias == "vw-server" {
			found = &e
			break
		}
	}
	if found == nil {
		t.Fatalf("vw-server not found in host list")
	}
	if found.Source == "vw:vw" {
		t.Errorf("Source should NOT be 'vw:vw', got %q", found.Source)
	}
	if found.Source != "vw" {
		t.Errorf("Source = %q, want 'vw'", found.Source)
	}
}

func TestApp_CreateConnection_VaultTarget_KeyAuth(t *testing.T) {
	symKey := bytes.Repeat([]byte{0x02}, 64)
	sess := &vaultclient.Session{
		AccessToken: "key-token",
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
		postedCipher.ID = "cipher-key-456"
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(postedCipher)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	cf := config.CustomFields{
		Host:      "host",
		User:      "user",
		Port:      "port",
		ProxyJump: "proxyjump",
		Type:      "type",
	}
	src := vaultadapter.NewSource("vw:work", sess, nil, cf)
	vc := vaultadapter.NewClient(src)

	vaults := []config.Vault{
		{Name: "work", Server: srv.URL, Email: "work@example.com"},
	}
	hl := hosts.NewList(nil)
	app := tviewui.New(hl, tviewui.Deps{
		VaultCli:     vc,
		CustomFields: cf,
	}, vaults)

	params := tviewui.CreateParams{
		Alias:    "vw-key-server",
		Target:   "vw:work",
		HostName: "10.1.1.1",
		User:     "deploy",
		Port:     "22",
		AuthKind: "key",
		KeyAlgo:  "ed25519",
	}

	err := app.HandleCreateConnection(params)
	if err != nil {
		t.Fatalf("HandleCreateConnection failed: %v", err)
	}

	if postedCipher.Type != 5 {
		t.Errorf("posted cipher Type = %d, want 5 (SSH-Key)", postedCipher.Type)
	}
	if postedCipher.SshKey == nil {
		t.Fatalf("expected posted cipher SshKey to be non-nil")
	}

	privKeyBytes, err := sess.DecryptField(postedCipher.SshKey.PrivateKey)
	if err != nil || !strings.Contains(string(privKeyBytes), "OPENSSH PRIVATE KEY") {
		t.Errorf("decrypted private key invalid: %s, err=%v", string(privKeyBytes), err)
	}
	pubKeyBytes, err := sess.DecryptField(postedCipher.SshKey.PublicKey)
	if err != nil || !strings.HasPrefix(string(pubKeyBytes), "ssh-ed25519 ") {
		t.Errorf("decrypted public key invalid: %s, err=%v", string(pubKeyBytes), err)
	}

	// BitWarden SSH-Key items require a non-empty keyFingerprint. VaultWarden
	// nulls the entire sshKey object when it is missing/empty, and the web
	// vault crashes parsing an empty one.
	if postedCipher.SshKey.KeyFingerprint == "" {
		t.Fatalf("expected keyFingerprint to be populated")
	}
	fpBytes, err := sess.DecryptField(postedCipher.SshKey.KeyFingerprint)
	if err != nil {
		t.Fatalf("decrypt keyFingerprint: %v", err)
	}
	if !strings.HasPrefix(string(fpBytes), "SHA256:") {
		t.Errorf("decrypted keyFingerprint = %q, want SHA256:... prefix", string(fpBytes))
	}
	// Unencrypted key: passphrase must not leak as an empty EncString.
	if postedCipher.SshKey.Passphrase != "" {
		t.Errorf("passphrase should be omitted for unencrypted key, got %q", postedCipher.SshKey.Passphrase)
	}

	// Verify custom fields
	customMap := make(map[string]string)
	for _, f := range postedCipher.Fields {
		k, _ := sess.DecryptField(f.Name)
		v, _ := sess.DecryptField(f.Value)
		customMap[string(k)] = string(v)
	}
	if customMap["host"] != "10.1.1.1" {
		t.Errorf("custom field 'host' = %q, want '10.1.1.1'", customMap["host"])
	}
	if !strings.EqualFold(customMap["type"], "SSH") {
		t.Errorf("custom field 'type' = %q, want 'SSH'", customMap["type"])
	}

	// Verify host list updated
	entries := app.HostList().All()
	var found *hosts.Entry
	for _, e := range entries {
		if e.Alias == "vw-key-server" && e.HostName == "10.1.1.1" {
			found = &e
			break
		}
	}
	if found == nil {
		t.Fatalf("vw-key-server host not found in host list: %+v", entries)
	}
	if found.Source != "vw:work" {
		t.Errorf("Source = %q, want 'vw:work'", found.Source)
	}
	if found.AuthKind != "key" {
		t.Errorf("AuthKind = %q, want 'key'", found.AuthKind)
	}
}

func TestApp_CreateModal_KeyShortcuts_N_and_A(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config")
	app := setupTestAppWithConfigPath(configPath)

	if app.InCreateModal() {
		t.Fatal("expected not in create modal initially")
	}

	// Press 'n' to open modal
	app.HostPane().TriggerCreate()
	if !app.InCreateModal() {
		t.Fatal("expected to be in create modal after TriggerCreate")
	}

	modal := app.CreateModal()
	if modal == nil {
		t.Fatal("expected active CreateModal instance")
	}

	// Cancel modal
	modal.TriggerCancel()
	if app.InCreateModal() {
		t.Fatal("expected create modal closed after cancel")
	}

	// Open again and submit
	app.HostPane().TriggerCreate()
	if !app.InCreateModal() {
		t.Fatal("expected to be in create modal after second TriggerCreate")
	}

	modal = app.CreateModal()
	modal.SetAlias("shortcut-node")
	modal.SetHostName("10.0.0.99")
	modal.SetUser("root")
	modal.SetAuthKind("password")
	modal.SetPassword("pwd")
	modal.Submit()

	if app.InCreateModal() {
		t.Fatal("expected create modal closed after successful submit")
	}

	// Verify created entry
	entries := app.HostList().All()
	found := false
	for _, e := range entries {
		if e.Alias == "shortcut-node" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("shortcut-node not found in host list")
	}
}

func TestApp_DeleteConnection_FileAndVault(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config")

	_ = sshconfig.AppendHostEntry(configPath, sshconfig.HostConfig{
		Alias:    "file-to-delete",
		HostName: "1.2.3.4",
	})

	hl := hosts.NewList([]hosts.Entry{
		{Alias: "file-to-delete", HostName: "1.2.3.4", Source: "file"},
		{Alias: "vault-to-delete", HostName: "5.6.7.8", Source: "vw"},
	})

	app := tviewui.New(hl, tviewui.Deps{}, nil)
	app.SetSSHConfigPathForTest(configPath)

	// Test DeleteModal lifecycle
	if app.InDeleteModal() {
		t.Fatal("expected not in delete modal initially")
	}

	app.ShowDeleteModal(hosts.Entry{Alias: "file-to-delete", Source: "file"})
	if !app.InDeleteModal() {
		t.Fatal("expected to be in delete modal")
	}

	if app.DeleteModal() == nil {
		t.Fatal("expected non-nil DeleteModal")
	}

	// Delete file connection
	err := app.HandleDeleteConnection(hosts.Entry{Alias: "file-to-delete", Source: "file"})
	if err != nil {
		t.Fatalf("HandleDeleteConnection file failed: %v", err)
	}

	app.CloseDeleteModal()
	if app.InDeleteModal() {
		t.Fatal("expected delete modal closed")
	}

	// Verify host list updated
	all := app.HostList().All()
	if len(all) != 1 || all[0].Alias != "vault-to-delete" {
		t.Errorf("expected only vault-to-delete in host list, got: %+v", all)
	}

	// Verify config file updated
	cfgContent, _ := os.ReadFile(configPath)
	if strings.Contains(string(cfgContent), "Host file-to-delete") {
		t.Errorf("expected file-to-delete block removed from config:\n%s", string(cfgContent))
	}
}

func TestApp_DeleteConnection_Vault_PurgesSourceCache(t *testing.T) {
	symKey := bytes.Repeat([]byte{0x02}, 64)
	sess := &vaultclient.Session{
		AccessToken: "delete-token",
		SymEnc:      symKey[:32],
		SymMac:      symKey[32:],
	}

	enc := func(plain string) string {
		s, err := sess.EncryptField(plain)
		if err != nil {
			t.Fatalf("EncryptField(%q): %v", plain, err)
		}
		return s
	}

	// Fake vault server: DELETE /api/ciphers/{id} = permanent delete.
	var deletedPath string
	var deletedAuth string
	mux := http.NewServeMux()
	mux.HandleFunc("/api/ciphers/vault-cipher-777", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			http.Error(w, "bad method", http.StatusMethodNotAllowed)
			return
		}
		deletedPath = r.URL.Path
		deletedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	cf := config.CustomFields{Host: "host", User: "user", Port: "port", ProxyJump: "proxyjump", Type: "type"}
	src := vaultadapter.NewSource("vw", sess, []vaultclient.Cipher{
		{
			ID:     "vault-cipher-777",
			Name:   enc("vault-to-delete"),
			Type:   5,
			SshKey: &vaultclient.SshKey{PrivateKey: enc("KEY")},
			Fields: []vaultclient.CustomField{
				{Name: enc("host"), Value: enc("5.6.7.8"), Type: 0},
			},
		},
	}, cf)
	vc := vaultadapter.NewClient(src)

	hl := hosts.NewList([]hosts.Entry{
		{Alias: "vault-to-delete", HostName: "5.6.7.8", Source: "vw"},
	})
	app := tviewui.New(hl, tviewui.Deps{VaultCli: vc, CustomFields: cf}, []config.Vault{
		{Name: "vw", Server: srv.URL, Email: "user@example.com"},
	})

	// Precondition: the item is visible in the source cache.
	itemsBefore, _ := src.Items()
	if len(itemsBefore) != 1 {
		t.Fatalf("expected 1 cached item before delete, got %d", len(itemsBefore))
	}

	err := app.HandleDeleteConnection(hosts.Entry{Alias: "vault-to-delete", HostName: "5.6.7.8", Source: "vw"})
	if err != nil {
		t.Fatalf("HandleDeleteConnection vault failed: %v", err)
	}

	// Server must receive the permanent DELETE for the exact cipher.
	if deletedPath != "/api/ciphers/vault-cipher-777" {
		t.Errorf("deleted path = %q, want /api/ciphers/vault-cipher-777", deletedPath)
	}
	if deletedAuth != "Bearer delete-token" {
		t.Errorf("auth = %q, want 'Bearer delete-token'", deletedAuth)
	}

	// The cipher must be purged from the source cache (no stale history).
	itemsAfter, _ := src.Items()
	if len(itemsAfter) != 0 {
		t.Fatalf("expected source cache empty after delete, got %d items", len(itemsAfter))
	}

	// And the host list entry must be gone.
	all := app.HostList().All()
	if len(all) != 0 {
		t.Errorf("expected host list empty after delete, got: %+v", all)
	}
}

func TestApp_UpdateConnection_FileTarget(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config")

	initial := `# Top comment
Host old-box
    # Inline comment
    HostName 1.2.3.4
    User deploy
    Port 22
    ForwardAgent yes

Host other-box
    HostName 9.9.9.9
`
	if err := os.WriteFile(configPath, []byte(initial), 0600); err != nil {
		t.Fatalf("write initial config: %v", err)
	}

	hl := hosts.NewList([]hosts.Entry{
		{Alias: "old-box", HostName: "1.2.3.4", User: "deploy", Port: "22", Source: "file", AuthKind: "key"},
		{Alias: "other-box", HostName: "9.9.9.9", Source: "file"},
	})

	app := tviewui.New(hl, tviewui.Deps{}, nil)
	app.SetSSHConfigPathForTest(configPath)

	// Test EditModal lifecycle
	if app.InEdit() {
		t.Fatal("expected not in edit modal initially")
	}

	app.ShowEditModal(hosts.Entry{Alias: "old-box", HostName: "1.2.3.4", User: "deploy", Port: "22", Source: "file", AuthKind: "key"})
	if !app.InEdit() {
		t.Fatal("expected to be in edit modal")
	}
	if app.EditModal() == nil {
		t.Fatal("expected non-nil EditModal")
	}

	params := tviewui.CreateParams{
		Alias:     "new-box",
		Target:    "file",
		HostName:  "1.2.3.99",
		User:      "admin",
		Port:      "2222",
		ProxyJump: "jumpbox",
		AuthKind:  "key",
	}

	err := app.HandleUpdateConnection(hosts.Entry{Alias: "old-box", Source: "file", AuthKind: "key"}, params)
	if err != nil {
		t.Fatalf("HandleUpdateConnection failed: %v", err)
	}

	app.CloseEditModal()
	if app.InEdit() {
		t.Fatal("expected edit modal closed")
	}

	// Verify host list replaced old-box with new-box
	all := app.HostList().All()
	if len(all) != 2 {
		t.Fatalf("expected 2 entries in host list, got %d", len(all))
	}
	if all[0].Alias != "new-box" || all[0].HostName != "1.2.3.99" || all[0].User != "admin" || all[0].Port != "2222" || all[0].ProxyJump != "jumpbox" {
		t.Errorf("unexpected updated entry in host list: %+v", all[0])
	}

	// Verify config file updated in-place with comments preserved
	cfgContent, _ := os.ReadFile(configPath)
	cfgStr := string(cfgContent)
	if !strings.Contains(cfgStr, "# Top comment") || !strings.Contains(cfgStr, "# Inline comment") || !strings.Contains(cfgStr, "ForwardAgent yes") {
		t.Errorf("expected comments and ForwardAgent preserved in:\n%s", cfgStr)
	}
	if strings.Contains(cfgStr, "Host old-box") || strings.Contains(cfgStr, "1.2.3.4") {
		t.Errorf("expected old-box and 1.2.3.4 replaced in:\n%s", cfgStr)
	}
	if !strings.Contains(cfgStr, "Host new-box") || !strings.Contains(cfgStr, "HostName 1.2.3.99") || !strings.Contains(cfgStr, "Port 2222") {
		t.Errorf("expected new-box in config:\n%s", cfgStr)
	}
}

func TestApp_UpdateConnection_FileTarget_SwitchAuthKind(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config")

	initial := `Host server-a
    HostName 10.0.0.1
    IdentityFile ~/.ssh/id_rsa_server_a
`
	_ = os.WriteFile(configPath, []byte(initial), 0600)

	hl := hosts.NewList([]hosts.Entry{
		{Alias: "server-a", HostName: "10.0.0.1", Source: "file", AuthKind: "key", IdentityFile: "~/.ssh/id_rsa_server_a"},
	})
	app := tviewui.New(hl, tviewui.Deps{}, nil)
	app.SetSSHConfigPathForTest(configPath)

	// 1. Switch from key to password -> IdentityFile dropped from config
	paramsPW := tviewui.CreateParams{
		Alias:    "server-a",
		Target:   "file",
		HostName: "10.0.0.1",
		AuthKind: "password",
	}
	err := app.HandleUpdateConnection(hosts.Entry{Alias: "server-a", Source: "file", AuthKind: "key", IdentityFile: "~/.ssh/id_rsa_server_a"}, paramsPW)
	if err != nil {
		t.Fatalf("HandleUpdateConnection key->pw failed: %v", err)
	}

	cfgContent, _ := os.ReadFile(configPath)
	if strings.Contains(string(cfgContent), "IdentityFile") {
		t.Errorf("expected IdentityFile dropped when switching to password auth:\n%s", string(cfgContent))
	}
	if app.HostList().All()[0].AuthKind != "password" {
		t.Errorf("expected AuthKind password, got %q", app.HostList().All()[0].AuthKind)
	}

	// 2. Switch from password to key -> generates new keypair file and adds IdentityFile
	paramsKey := tviewui.CreateParams{
		Alias:    "server-a",
		Target:   "file",
		HostName: "10.0.0.1",
		AuthKind: "key",
		KeyAlgo:  "ed25519",
	}
	err = app.HandleUpdateConnection(hosts.Entry{Alias: "server-a", Source: "file", AuthKind: "password"}, paramsKey)
	if err != nil {
		t.Fatalf("HandleUpdateConnection pw->key failed: %v", err)
	}

	cfgContent2, _ := os.ReadFile(configPath)
	if !strings.Contains(string(cfgContent2), "IdentityFile") {
		t.Errorf("expected IdentityFile added when switching to key auth:\n%s", string(cfgContent2))
	}
	if app.HostList().All()[0].AuthKind != "key" {
		t.Errorf("expected AuthKind key, got %q", app.HostList().All()[0].AuthKind)
	}
}

func TestApp_UpdateConnection_VaultTarget(t *testing.T) {
	symKey := bytes.Repeat([]byte{0x03}, 64)
	sess := &vaultclient.Session{
		AccessToken: "update-token",
		SymEnc:      symKey[:32],
		SymMac:      symKey[32:],
	}

	enc := func(plain string) string {
		s, err := sess.EncryptField(plain)
		if err != nil {
			t.Fatalf("EncryptField(%q): %v", plain, err)
		}
		return s
	}

	var putPath string
	var putAuth string
	var putCipher vaultclient.Cipher

	mux := http.NewServeMux()
	mux.HandleFunc("/api/ciphers/vault-cipher-999", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			http.Error(w, "bad method", http.StatusMethodNotAllowed)
			return
		}
		putPath = r.URL.Path
		putAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&putCipher)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(putCipher)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	cf := config.CustomFields{Host: "host", User: "user", Port: "port", ProxyJump: "proxyjump", Type: "type"}
	src := vaultadapter.NewSource("vw", sess, []vaultclient.Cipher{
		{
			ID:     "vault-cipher-999",
			Name:   enc("vault-orig"),
			Type:   1,
			Login:  &vaultclient.Login{Username: enc("olduser"), Password: enc("oldpass")},
			Fields: []vaultclient.CustomField{
				{Name: enc("host"), Value: enc("10.0.0.1"), Type: 0},
				{Name: enc("type"), Value: enc("SSH"), Type: 0},
				{Name: enc("custom_note"), Value: enc("keep_me"), Type: 0}, // Unmanaged custom field
			},
		},
	}, cf)
	vc := vaultadapter.NewClient(src)

	hl := hosts.NewList([]hosts.Entry{
		{Alias: "vault-orig", HostName: "10.0.0.1", User: "olduser", Source: "vw", AuthKind: "password"},
	})
	app := tviewui.New(hl, tviewui.Deps{VaultCli: vc, CustomFields: cf}, []config.Vault{
		{Name: "vw", Server: srv.URL, Email: "user@example.com"},
	})

	params := tviewui.CreateParams{
		Alias:    "vault-renamed",
		Target:   "vw",
		HostName: "10.0.0.99",
		User:     "newuser",
		Port:     "2200",
		AuthKind: "password",
		Password: "newsecretpassword",
	}

	err := app.HandleUpdateConnection(hosts.Entry{Alias: "vault-orig", HostName: "10.0.0.1", Source: "vw", AuthKind: "password"}, params)
	if err != nil {
		t.Fatalf("HandleUpdateConnection vault failed: %v", err)
	}

	if putPath != "/api/ciphers/vault-cipher-999" {
		t.Errorf("putPath = %q, want /api/ciphers/vault-cipher-999", putPath)
	}
	if putAuth != "Bearer update-token" {
		t.Errorf("putAuth = %q, want 'Bearer update-token'", putAuth)
	}

	// Verify decrypted name
	nameBytes, _ := sess.DecryptField(putCipher.Name)
	if string(nameBytes) != "vault-renamed" {
		t.Errorf("decrypted put Name = %q, want 'vault-renamed'", string(nameBytes))
	}

	// Verify unmanaged custom field was preserved
	foundCustomNote := false
	for _, f := range putCipher.Fields {
		nBytes, _ := sess.DecryptField(f.Name)
		if string(nBytes) == "custom_note" {
			vBytes, _ := sess.DecryptField(f.Value)
			if string(vBytes) == "keep_me" {
				foundCustomNote = true
			}
		}
	}
	if !foundCustomNote {
		t.Errorf("expected unmanaged custom field 'custom_note: keep_me' to be preserved in PUT body")
	}

	// Verify host list updated
	all := app.HostList().All()
	if len(all) != 1 || all[0].Alias != "vault-renamed" || all[0].HostName != "10.0.0.99" || all[0].User != "newuser" || all[0].Port != "2200" {
		t.Errorf("unexpected updated host list entry: %+v", all)
	}
}

func TestAppScopeModalLifecycle(t *testing.T) {
	hl := sampleHostList()
	app := tviewui.New(hl, tviewui.Deps{}, nil)
	if app.InScopeModal() {
		t.Fatal("precondition: not in scope modal")
	}
	app.ShowScopeModalForTest()
	if !app.InScopeModal() {
		t.Fatal("expected scope modal open after ShowScopeModalForTest")
	}
	app.CancelScopeModal()
	if app.InScopeModal() {
		t.Fatal("expected scope modal dismissed after cancel")
	}
}

func TestAppScopeModalSelectChangesScope(t *testing.T) {
	hl := sampleHostList()
	app := tviewui.New(hl, tviewui.Deps{}, nil)
	before := app.HostPane().CurrentScope()
	app.ShowScopeModalForTest()
	// Move down so the selection is NOT the current (first) scope.
	app.ScopeModal().TriggerKey(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone))
	app.ConfirmScopeModalSelect()
	if app.InScopeModal() {
		t.Fatal("expected scope modal closed after select")
	}
	if app.HostPane().CurrentScope() == before {
		t.Errorf("scope did not change: %q", before)
	}
}

var _ = tcell.KeyCtrlB

// TestAppCtrlShiftQHardClosesActiveSession: Ctrl+Shift+Q immediately terminates
// a hung or idle active session without waiting for confirmation or remote shell.
func TestAppCtrlShiftQHardClosesActiveSession(t *testing.T) {
	hl := sampleHostList()
	app := tviewui.New(hl, tviewui.Deps{}, nil)
	key := tviewui.SessionKey("prod-db-01", "file")
	app.TerminalPane().SetSessionForTest(key, "prod-db-01", "file")
	hl.MarkLive("prod-db-01", "file")
	app.ShowTerminalPaneForTest()

	if !app.TerminalPane().IsRunning() {
		t.Fatal("precondition: session must be running")
	}

	// Press Ctrl+Shift+Q
	ev := tcell.NewEventKey(tcell.KeyCtrlQ, 'Q', tcell.ModCtrl|tcell.ModShift)
	app.HandleGlobalKey(ev)

	if app.TerminalPane().IsRunning() {
		t.Error("expected session terminated after Ctrl+Shift+Q")
	}
	if isLive(hl, "prod-db-01", "file") {
		t.Error("expected host marked dead after hard close")
	}
	if app.FocusedPane() != "host" {
		t.Errorf("FocusedPane = %q, want host", app.FocusedPane())
	}
}

// TestAppCtrlQQuits: Ctrl+Q is a global safe-exit shortcut (revamp keymap).
func TestAppCtrlQQuits(t *testing.T) {
	hl := sampleHostList()
	app := tviewui.New(hl, tviewui.Deps{}, nil)
	app.HandleGlobalKey(tcell.NewEventKey(tcell.KeyCtrlQ, 0, tcell.ModNone))
	if !app.InQuitModal() {
		t.Fatal("expected quit modal after Ctrl+Q")
	}
}

// TestAppSlashFocusesFilter: '/' jumps to the filter search input (global).
func TestAppSlashFocusesFilter(t *testing.T) {
	hl := sampleHostList()
	app := tviewui.New(hl, tviewui.Deps{}, nil)
	if app.HostPane().FilterFocused() {
		t.Fatal("precondition: filter not focused")
	}
	app.HandleGlobalKey(tcell.NewEventKey(tcell.KeyRune, '/', tcell.ModNone))
	if !app.HostPane().FilterFocused() {
		t.Error("expected '/' to focus the filter input")
	}
}

// TestAppQuestionOpensHelp: '?' opens the interactive help sheet; closing it
// returns focus to the host list.
func TestAppQuestionOpensHelp(t *testing.T) {
	hl := sampleHostList()
	app := tviewui.New(hl, tviewui.Deps{}, nil)
	if app.InHelp() {
		t.Fatal("precondition: help closed")
	}
	app.HandleGlobalKey(tcell.NewEventKey(tcell.KeyRune, '?', tcell.ModNone))
	if !app.InHelp() {
		t.Fatal("expected '?' to open help")
	}
	app.CancelHelpModal()
	if app.InHelp() {
		t.Fatal("expected help dismissed after cancel")
	}
}

func TestApp_UpdateConnection_Refusals(t *testing.T) {
	hl := hosts.NewList([]hosts.Entry{
		{Alias: "live-server", HostName: "1.1.1.1", Source: "file", Live: true},
		{Alias: "*.wildcard", HostName: "2.2.2.2", Source: "file", Wildcard: true},
	})
	app := tviewui.New(hl, tviewui.Deps{}, nil)

	// Refusal 1: Live connection
	app.ShowEditModal(hosts.Entry{Alias: "live-server", Source: "file", Live: true})
	if app.InEdit() {
		t.Errorf("expected edit modal to be refused for live connection")
	}

	// Refusal 2: Wildcard connection
	app.ShowEditModal(hosts.Entry{Alias: "*.wildcard", Source: "file", Wildcard: true})
	if app.InEdit() {
		t.Errorf("expected edit modal to be refused for wildcard connection")
	}
}

