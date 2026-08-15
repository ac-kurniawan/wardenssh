package tviewui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/ac-kurniawan/wardenssh/internal/config"
	"github.com/ac-kurniawan/wardenssh/internal/connect"
	"github.com/ac-kurniawan/wardenssh/internal/hosts"
	"github.com/ac-kurniawan/wardenssh/internal/session"
	"github.com/ac-kurniawan/wardenssh/internal/sshagent"
	"github.com/ac-kurniawan/wardenssh/internal/sshconfig"
	"github.com/ac-kurniawan/wardenssh/internal/vault"
	"github.com/ac-kurniawan/wardenssh/internal/vaultadapter"
	"github.com/ac-kurniawan/wardenssh/internal/vaultclient"
)

// Deps holds injected dependencies for the TUI app.
type Deps struct {
	Agent        *sshagent.Keyring
	Mgr          *session.Manager
	VaultCli     vault.Client
	AgentPipe    string
	CustomFields config.CustomFields
	NoKeyring    bool // true = skip OS keyring, always prompt for master password
}

// App is the main WardenSSH launcher TUI application.
type App struct {
	app      *tview.Application
	hostList *hosts.List
	deps     Deps
	vaults   []config.Vault

	hostPane    *HostListPane
	termPane    *TerminalPane
	setupModal  *SetupModal
	quitModal   *QuitModal
	discModal   *DisconnectModal
	createModal *CreateModal
	deleteModal *DeleteModal
	footer      *Footer

	root    *tview.Flex
	left    *tview.Flex
	right   *tview.Flex
	overlay *tview.Pages

	mu            sync.Mutex
	inSetup       bool
	inQuit        bool
	inDisconnect  bool
	inCreate      bool
	inDelete      bool
	sshConfigPath string
	termFocused   bool // focus is on the terminal pane (vs. the host list)
	syncStarted   bool
	syncTicker    *time.Ticker
	stopSync      chan struct{}
}

// New creates the TUI app. If vaults is non-empty, it starts in setup mode.
func New(hostList *hosts.List, deps Deps, vaults []config.Vault) *App {
	SetBlockCursor()

	a := &App{
		app:      tview.NewApplication(),
		hostList: hostList,
		deps:     deps,
		vaults:   vaults,
	}
	// Mouse is required for the terminal pane's scrollback scrolling (wheel)
	// and for standard mouse navigation (click to focus, wheel to scroll).
	a.app.EnableMouse(true)

	// Build panes.
	a.hostPane = NewHostListPane(hostList)
	a.termPane = NewTerminalPane(a.app)
	a.hostPane.Refresh()

	// Wire host pane callbacks.
	a.hostPane.SetOnConnect(a.handleConnect)
	a.hostPane.SetOnScopeChange(func() {})
	a.hostPane.SetOnRefresh(func() { _ = a.TriggerSync() })
	a.hostPane.SetOnCreate(a.showCreateModal)
	a.hostPane.SetOnDelete(a.showDeleteModal)

	// Layout: left = host list, right = terminal (hidden initially).
	a.left = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(a.hostPane.Primitive(), 0, 1, true)

	a.right = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(a.termPane.Primitive(), 0, 1, false)

	// Content: column flex. Left takes full width when no session; right
	// appears when a session starts.
	content := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(a.left, 0, 1, true)

	a.root = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(content, 0, 1, true)

	// Footer hotkey hint (fixed one-line bar at the bottom).
	a.footer = NewFooter()
	a.root.AddItem(a.footer.Primitive(), 1, 0, false)

	// Pages for modal overlays (setup, quit).
	a.overlay = tview.NewPages().
		AddPage("main", a.root, true, true)
	a.app.SetRoot(a.overlay, true)

	// Global key handling.
	a.app.SetInputCapture(a.handleGlobalKeys)

	// Setup mode?
	if len(vaults) > 0 {
		a.inSetup = true
		a.setupModal = NewSetupModal(vaults, deps.CustomFields, hostList, deps.NoKeyring)
		a.setupModal.SetOnComplete(func(vc vault.Client) {
			a.app.QueueUpdateDraw(func() {
				a.inSetup = false
				a.deps.VaultCli = vc
				a.overlay.RemovePage("setup")
				a.hostPane.Refresh()
				a.app.SetFocus(a.hostPane.Primitive())
			})
			a.StartBackgroundSync(5 * time.Minute)
		})
		a.setupModal.SetOnSkip(func() {
			a.inSetup = false
			a.overlay.RemovePage("setup")
			a.app.SetFocus(a.hostPane.Primitive())
		})
		a.overlay.AddPage("setup", a.setupModal.Primitive(), true, true)
		a.app.SetFocus(a.setupModal.Primitive())
	}

	return a
}

// Run starts the tview application.
func (a *App) Run() error {
	return a.app.Run()
}

// HostPane returns the host list pane.
func (a *App) HostPane() *HostListPane { return a.hostPane }

// TerminalPane returns the terminal pane.
func (a *App) TerminalPane() *TerminalPane { return a.termPane }

// FooterText returns the current footer hotkey hint (used in tests).
func (a *App) FooterText() string {
	if a.footer == nil {
		return ""
	}
	return a.footer.Text()
}

// FocusTerminal moves keyboard focus to the terminal pane (right side). The
// session, if any, keeps running.
func (a *App) FocusTerminal() {
	a.termFocused = true
	a.footer.SetMode("terminal")
	a.app.SetFocus(a.termPane.Primitive())
}

// FocusHostList moves keyboard focus back to the host list (left side). The
// right pane stays visible and the session, if any, keeps running
// (Q18/iii yield-and-switch: "move left → kembali ke list (sesi tetap jalan)").
func (a *App) FocusHostList() {
	a.termFocused = false
	a.footer.SetMode("host")
	a.app.SetFocus(a.hostPane.Primitive())
}

// FocusedPane reports which pane currently owns keyboard focus: "terminal" or
// "host".
func (a *App) FocusedPane() string {
	if a.termFocused {
		return "terminal"
	}
	return "host"
}

// InSetup reports whether the setup modal is active.
func (a *App) InSetup() bool { return a.inSetup }

// InQuitModal reports whether the quit modal is active.
func (a *App) InQuitModal() bool { return a.inQuit }

// SkipSetup skips all vault setup (for tests / Esc key).
func (a *App) SkipSetup() {
	if a.setupModal != nil {
		a.setupModal.SkipCurrent()
	}
	a.inSetup = false
	a.overlay.RemovePage("setup")
}

// TriggerSync performs vault sync in a background goroutine and updates the
// host pane sync status header and host entries upon completion. Returns a channel
// that closes when the sync finishes.
func (a *App) TriggerSync() <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		if a.deps.VaultCli == nil {
			return
		}

		var err error
		if vAdapterClient, ok := a.deps.VaultCli.(*vaultadapter.Client); ok {
			var vc *vaultclient.Client
			if len(a.vaults) > 0 {
				vc = vaultclientNew(a.vaults[0].Server)
			}
			err = vAdapterClient.SyncAll(vc)
		} else {
			err = a.deps.VaultCli.Sync()
		}

		if err == nil {
			if vEntries, errEntries := appVaultEntries(a.deps.VaultCli); errEntries == nil {
				for _, src := range a.deps.VaultCli.Sources() {
					var srcEntries []hosts.Entry
					for _, e := range vEntries {
						if e.Source == src.Name() {
							srcEntries = append(srcEntries, e)
						}
					}
					a.hostList.ReplaceVaultEntries(src.Name(), srcEntries)
				}
			}
		}

		var status string
		if err != nil {
			status = "[red]Sync failed (offline)[-]"
		} else {
			status = fmt.Sprintf("Synced %s", time.Now().Format("15:04"))
		}

		a.queueUpdateDraw(func() {
			a.hostPane.SetSyncStatus(status)
			a.hostPane.Refresh()
		})
	}()
	return done
}

func (a *App) queueUpdateDraw(fn func()) {
	if fn != nil {
		fn()
	}
}

// StartBackgroundSync starts a background ticker with the given interval that triggers vault sync.
func (a *App) StartBackgroundSync(interval time.Duration) {
	if interval <= 0 {
		return
	}
	a.mu.Lock()
	if a.syncStarted {
		a.mu.Unlock()
		return
	}
	a.syncStarted = true
	ticker := time.NewTicker(interval)
	a.syncTicker = ticker
	a.stopSync = make(chan struct{})
	a.mu.Unlock()

	go func() {
		for {
			select {
			case <-ticker.C:
				a.TriggerSync()
			case <-a.stopSync:
				ticker.Stop()
				return
			}
		}
	}()
}

// StopBackgroundSync stops the background sync ticker if running.
func (a *App) StopBackgroundSync() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.syncStarted {
		if a.stopSync != nil {
			close(a.stopSync)
		}
		a.syncStarted = false
	}
}

// RequestQuit always opens the quit-confirmation modal (Q31/C). It never
// quits directly: the user must confirm via the modal (Kill all / Detach /
// Cancel). Returns false — the caller should not quit immediately.
func (a *App) RequestQuit() bool {
	a.showQuitModal()
	return false
}

// CancelQuit dismisses the quit modal without quitting (modal 'c' / Esc).
// Exported for tests and programmatic dismissal.
func (a *App) CancelQuit() {
	if !a.inQuit {
		return
	}
	a.inQuit = false
	a.overlay.RemovePage("quit")
	a.app.SetFocus(a.hostPane.Primitive())
}

// KillAllQuit kills all sessions, clears live flags, and quits (modal 'k').
// Exported for tests and programmatic triggering.
func (a *App) KillAllQuit() {
	a.inQuit = false
	a.overlay.RemovePage("quit")
	a.termPane.Close()
	if a.deps.Mgr != nil {
		a.deps.Mgr.KillAll()
	}
	a.clearAllLive()
	a.app.Stop()
}

// DetachQuit quits leaving sessions agentless (modal 'd'). Exported for tests.
func (a *App) DetachQuit() {
	a.inQuit = false
	a.overlay.RemovePage("quit")
	a.app.Stop()
}

// HandleGlobalKey routes a key event through the app-level input capture.
// Exported as a test seam (mirrors HostListPane.HandleFilterKey).
func (a *App) HandleGlobalKey(event *tcell.EventKey) *tcell.EventKey {
	return a.handleGlobalKeys(event)
}

// HandleConnectForTest routes a host selection through the app's connect
// handler (tests only; mirrors HostListPane.TriggerConnect).
func (a *App) HandleConnectForTest(entry hosts.Entry) {
	a.handleConnect(entry)
}

// InDisconnect reports whether the disconnect confirmation modal is open.
func (a *App) InDisconnect() bool { return a.inDisconnect }

// InCreateModal reports whether the connection creation modal is active.
func (a *App) InCreateModal() bool { return a.inCreate }

// CreateModal returns the current CreateModal instance.
func (a *App) CreateModal() *CreateModal { return a.createModal }

// ShowCreateModal opens the connection creation modal.
func (a *App) ShowCreateModal() { a.showCreateModal() }

// CloseCreateModal closes the connection creation modal and returns focus to the host list.
func (a *App) CloseCreateModal() {
	a.inCreate = false
	a.overlay.RemovePage("create")
	a.hostPane.Refresh()
	a.app.SetFocus(a.hostPane.Primitive())
}

// InDeleteModal reports whether the delete confirmation modal is active.
func (a *App) InDeleteModal() bool { return a.inDelete }

// DeleteModal returns the current DeleteModal instance.
func (a *App) DeleteModal() *DeleteModal { return a.deleteModal }

// ShowDeleteModal opens the delete confirmation modal for a given host entry.
func (a *App) ShowDeleteModal(entry hosts.Entry) { a.showDeleteModal(entry) }

// CloseDeleteModal closes the delete confirmation modal.
func (a *App) CloseDeleteModal() {
	if a.inDelete {
		a.inDelete = false
		a.overlay.RemovePage("delete")
		a.hostPane.Refresh()
		a.app.SetFocus(a.hostPane.Primitive())
	}
}

func (a *App) showDeleteModal(entry hosts.Entry) {
	if a.inDelete {
		return
	}
	a.inDelete = true
	a.deleteModal = NewDeleteModal(entry.Alias, entry.Source)
	a.deleteModal.SetOnDelete(func() {
		_ = a.HandleDeleteConnection(entry)
		a.CloseDeleteModal()
	})
	a.deleteModal.SetOnCancel(a.CloseDeleteModal)
	a.overlay.AddPage("delete", a.deleteModal.Primitive(), true, true)
	a.app.SetFocus(a.deleteModal.Primitive())
}

// SetSSHConfigPathForTest overrides the ssh config path used when writing host entries (tests only).
func (a *App) SetSSHConfigPathForTest(path string) {
	a.sshConfigPath = path
}

func (a *App) showCreateModal() {
	if a.inCreate {
		return
	}
	targets := a.availableTargets()
	a.createModal = NewCreateModal(targets)
	a.createModal.SetOnSubmit(func(params CreateParams) error {
		if err := a.HandleCreateConnection(params); err != nil {
			return err
		}
		a.CloseCreateModal()
		return nil
	})
	a.createModal.SetOnCancel(a.CloseCreateModal)
	a.inCreate = true
	a.overlay.AddPage("create", a.createModal.Primitive(), true, true)
	a.app.SetFocus(a.createModal.Primitive())
}

func (a *App) availableTargets() []string {
	targets := []string{"~/.ssh/config"}
	seen := make(map[string]bool)
	seen["~/.ssh/config"] = true

	if a.deps.VaultCli != nil {
		for _, src := range a.deps.VaultCli.Sources() {
			name := src.Name()
			if name != "" && !seen[name] {
				seen[name] = true
				targets = append(targets, name)
			}
		}
	}
	for _, v := range a.vaults {
		name := v.Name
		if name != "" && !seen[name] {
			seen[name] = true
			targets = append(targets, name)
		}
	}
	return targets
}

// HandleCreateConnection creates a new SSH connection either in ~/.ssh/config or in a Vault.
func (a *App) HandleCreateConnection(params CreateParams) error {
	params.Target = strings.TrimSpace(params.Target)
	if params.Target == "" || params.Target == "~/.ssh/config" || params.Target == "file" {
		return a.handleCreateFileConnection(params)
	}
	return a.handleCreateVaultConnection(params)
}

func (a *App) handleCreateFileConnection(params CreateParams) error {
	identityPath := ""
	authKind := params.AuthKind
	if authKind == "" {
		authKind = "key"
	}

	if authKind == "key" {
		algo := params.KeyAlgo
		if algo == "" {
			algo = "ed25519"
		}
		var sshDir string
		if a.sshConfigPath != "" {
			sshDir = filepath.Dir(a.sshConfigPath)
		} else {
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("resolve home dir: %w", err)
			}
			sshDir = filepath.Join(home, ".ssh")
		}
		identityPath = filepath.Join(sshDir, fmt.Sprintf("id_%s_%s", algo, params.Alias))
		if err := sshconfig.GenerateKeyToFile(algo, identityPath); err != nil {
			return fmt.Errorf("generate key: %w", err)
		}
	}

	configPath := a.sshConfigPath
	if configPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("resolve home dir: %w", err)
		}
		configPath = filepath.Join(home, ".ssh", "config")
	}

	cfg := sshconfig.HostConfig{
		Alias:        params.Alias,
		HostName:     params.HostName,
		User:         params.User,
		Port:         params.Port,
		ProxyJump:    params.ProxyJump,
		IdentityFile: identityPath,
	}

	if err := sshconfig.AppendHostEntry(configPath, cfg); err != nil {
		return fmt.Errorf("append config entry: %w", err)
	}

	newEntry := hosts.Entry{
		Alias:        params.Alias,
		HostName:     params.HostName,
		User:         params.User,
		Port:         params.Port,
		ProxyJump:    params.ProxyJump,
		Source:       "file",
		IdentityFile: identityPath,
		AuthKind:     authKind,
	}
	a.hostList.Merge([]hosts.Entry{newEntry})
	a.hostPane.Refresh()
	return nil
}

func (a *App) handleCreateVaultConnection(params CreateParams) error {
	vaultName := strings.TrimPrefix(params.Target, "vw:")
	var sess *vaultclient.Session
	var cf config.CustomFields
	var targetSource *vaultadapter.Source
	var serverURL string

	if a.deps.VaultCli != nil {
		if vAdapterClient, ok := a.deps.VaultCli.(*vaultadapter.Client); ok {
			targetSource = vAdapterClient.SourceByName(params.Target)
			if targetSource == nil {
				targetSource = vAdapterClient.SourceByName(vaultName)
			}
			if targetSource != nil {
				sess = targetSource.Session()
				cf = targetSource.Fields()
			}
		}
	}

	// Fallback custom fields from deps if not retrieved from source
	if cf.Host == "" {
		cf = a.deps.CustomFields
	}
	if cf.Host == "" {
		cf.Host = "host"
	}
	if cf.User == "" {
		cf.User = "user"
	}
	if cf.Port == "" {
		cf.Port = "port"
	}
	if cf.ProxyJump == "" {
		cf.ProxyJump = "proxyjump"
	}
	if cf.Type == "" {
		cf.Type = "type"
	}

	// Find server URL from configured vaults
	for _, v := range a.vaults {
		if v.Name == vaultName || v.Name == params.Target || "vw:"+v.Name == params.Target {
			serverURL = v.Server
			break
		}
	}
	if serverURL == "" && len(a.vaults) > 0 {
		serverURL = a.vaults[0].Server
	}

	if sess == nil {
		return fmt.Errorf("vault %q is not unlocked or unavailable", params.Target)
	}

	vc := vaultclientNew(serverURL)

	encName, err := sess.EncryptField(params.Alias)
	if err != nil {
		return fmt.Errorf("encrypt name: %w", err)
	}

	// Build custom fields
	var customFields []vaultclient.CustomField
	fHost, err := encryptCustomField(sess, cf.Host, params.HostName)
	if err != nil {
		return fmt.Errorf("encrypt host field: %w", err)
	}
	customFields = append(customFields, fHost)

	fType, err := encryptCustomField(sess, cf.Type, "SSH")
	if err != nil {
		return fmt.Errorf("encrypt type field: %w", err)
	}
	customFields = append(customFields, fType)

	if params.User != "" {
		fUser, err := encryptCustomField(sess, cf.User, params.User)
		if err != nil {
			return fmt.Errorf("encrypt user field: %w", err)
		}
		customFields = append(customFields, fUser)
	}
	if params.Port != "" {
		fPort, err := encryptCustomField(sess, cf.Port, params.Port)
		if err != nil {
			return fmt.Errorf("encrypt port field: %w", err)
		}
		customFields = append(customFields, fPort)
	}
	if params.ProxyJump != "" {
		fPJ, err := encryptCustomField(sess, cf.ProxyJump, params.ProxyJump)
		if err != nil {
			return fmt.Errorf("encrypt proxyjump field: %w", err)
		}
		customFields = append(customFields, fPJ)
	}

	authKind := params.AuthKind
	if authKind == "" {
		authKind = "key"
	}

	var cipherItem vaultclient.Cipher
	if authKind == "password" {
		encUser, err := sess.EncryptField(params.User)
		if err != nil {
			return fmt.Errorf("encrypt username: %w", err)
		}
		encPass, err := sess.EncryptField(params.Password)
		if err != nil {
			return fmt.Errorf("encrypt password: %w", err)
		}
		cipherItem = vaultclient.Cipher{
			Name: encName,
			Type: 1,
			Login: &vaultclient.Login{
				Username: encUser,
				Password: encPass,
			},
			Fields: customFields,
		}
	} else {
		algo := params.KeyAlgo
		if algo == "" {
			algo = "ed25519"
		}
		privPEM, pubAuth, fingerprint, err := sshconfig.GenerateKeyPair(algo)
		if err != nil {
			return fmt.Errorf("generate keypair: %w", err)
		}
		encPriv, err := sess.EncryptField(privPEM)
		if err != nil {
			return fmt.Errorf("encrypt private key: %w", err)
		}
		encPub, err := sess.EncryptField(pubAuth)
		if err != nil {
			return fmt.Errorf("encrypt public key: %w", err)
		}
		encFp, err := sess.EncryptField(fingerprint)
		if err != nil {
			return fmt.Errorf("encrypt key fingerprint: %w", err)
		}
		cipherItem = vaultclient.Cipher{
			Name: encName,
			Type: 5,
			SshKey: &vaultclient.SshKey{
				PrivateKey:     encPriv,
				PublicKey:      encPub,
				KeyFingerprint: encFp,
			},
			Fields: customFields,
		}
	}

	created, err := vc.CreateCipher(sess, cipherItem)
	if err != nil {
		return fmt.Errorf("create cipher: %w", err)
	}

	if targetSource != nil && created != nil {
		targetSource.AddCipher(*created)
	}

	sourceLabel := params.Target
	if targetSource != nil {
		sourceLabel = targetSource.Name()
	}

	newEntry := hosts.Entry{
		Alias:     params.Alias,
		HostName:  params.HostName,
		User:      params.User,
		Port:      params.Port,
		ProxyJump: params.ProxyJump,
		Source:    sourceLabel,
		AuthKind:  authKind,
	}
	a.hostList.Merge([]hosts.Entry{newEntry})
	a.hostPane.Refresh()
	return nil
}

// HandleDeleteConnection deletes an SSH connection entry from ~/.ssh/config or Vault.
func (a *App) HandleDeleteConnection(entry hosts.Entry) error {
	if entry.Source == "file" || entry.Source == "~/.ssh/config" {
		configPath := a.sshConfigPath
		if configPath == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("resolve home dir: %w", err)
			}
			configPath = filepath.Join(home, ".ssh", "config")
		}
		if err := sshconfig.DeleteHostEntry(configPath, entry.Alias); err != nil {
			return fmt.Errorf("delete ssh config entry: %w", err)
		}
	} else {
		if a.deps.VaultCli != nil {
			if vAdapterClient, ok := a.deps.VaultCli.(*vaultadapter.Client); ok {
				targetSource := vAdapterClient.SourceByName(entry.Source)
				if targetSource != nil {
					items, err := targetSource.Items()
					if err == nil {
						for _, item := range items {
							if item.Name == entry.Alias {
								sess := targetSource.Session()
								var serverURL string
								for _, v := range a.vaults {
									if v.Name == entry.Source || "vw:"+v.Name == entry.Source {
										serverURL = v.Server
										break
									}
								}
								if serverURL == "" && len(a.vaults) > 0 {
									serverURL = a.vaults[0].Server
								}
								if sess != nil {
									vc := vaultclientNew(serverURL)
									if err := vc.DeleteCipher(sess, item.ID); err != nil {
										return fmt.Errorf("delete vault cipher: %w", err)
									}
									// Permanent delete is done server-side; purge the
									// local cache too so the item never resurfaces.
									targetSource.RemoveCipher(item.ID)
								}
								break
							}
						}
					}
				}
			}
		}
	}

	a.hostList.Remove(entry.Alias, entry.Source)
	a.hostPane.Refresh()
	return nil
}

func encryptCustomField(sess *vaultclient.Session, name, val string) (vaultclient.CustomField, error) {
	encName, err := sess.EncryptField(name)
	if err != nil {
		return vaultclient.CustomField{}, err
	}
	encVal, err := sess.EncryptField(val)
	if err != nil {
		return vaultclient.CustomField{}, err
	}
	return vaultclient.CustomField{
		Name:  encName,
		Value: encVal,
		Type:  0,
	}, nil
}

// ConfirmDisconnect confirms the disconnect modal (tests / modal 'y').
func (a *App) ConfirmDisconnect() {
	if !a.inDisconnect || a.discModal == nil {
		return
	}
	a.discModal.TriggerDisconnect()
}

// CancelDisconnect dismisses the disconnect modal (tests / modal 'n'/Esc).
func (a *App) CancelDisconnect() {
	if !a.inDisconnect || a.discModal == nil {
		return
	}
	a.discModal.TriggerCancel()
}

// HostList returns the underlying host list (used by tests).
func (a *App) HostList() *hosts.List { return a.hostList }

func (a *App) hasLiveSessions() bool {
	for _, e := range a.hostList.All() {
		if e.Live {
			return true
		}
	}
	return false
}

func (a *App) showQuitModal() {
	if a.inQuit {
		return
	}
	a.inQuit = true
	a.quitModal = NewQuitModal()
	a.quitModal.SetOnKillAll(a.KillAllQuit)
	a.quitModal.SetOnDetach(a.DetachQuit)
	a.quitModal.SetOnCancel(a.CancelQuit)
	a.overlay.AddPage("quit", a.quitModal.Primitive(), true, true)
	a.app.SetFocus(a.quitModal.Primitive())
}

// showDisconnectModal opens the disconnect-confirmation modal for a host whose
// session is the currently displayed one. Confirm closes only that session;
// Cancel leaves it running.
func (a *App) showDisconnectModal(entry hosts.Entry) {
	if a.inDisconnect {
		return
	}
	a.inDisconnect = true
	a.discModal = NewDisconnectModal(entry.Alias)
	a.discModal.SetOnDisconnect(func() {
		key := SessionKey(entry.Alias, entry.Source)
		a.termPane.CloseSession(key)
		if entry.Source != "file" && a.deps.Agent != nil {
			_ = a.deps.Agent.ReleaseSession(key)
		}
		a.hostList.MarkDead(entry.Alias, entry.Source)
		a.hostPane.Refresh()
		a.inDisconnect = false
		a.overlay.RemovePage("disconnect")
		if a.termPane.SessionCount() == 0 {
			a.showHostListPane()
			return
		}
		a.termPane.SyncToMostRecent()
		a.showTerminalPane()
	})
	a.discModal.SetOnCancel(func() {
		a.inDisconnect = false
		a.overlay.RemovePage("disconnect")
		a.FocusHostList()
	})
	a.overlay.AddPage("disconnect", a.discModal.Primitive(), true, true)
	a.app.SetFocus(a.discModal.Primitive())
}

func (a *App) clearAllLive() {
	for _, e := range a.hostList.All() {
		if e.Live {
			a.hostList.MarkDead(e.Alias, e.Source)
		}
	}
}

// handleConnect is called when Enter is pressed on a host entry.
//   - The host already has the ACTIVE session -> disconnect confirmation modal.
//   - The host has a BACKGROUND session -> yield-and-switch to it (no spawn).
//   - Otherwise -> spawn a new session; previous sessions keep running.
func (a *App) handleConnect(entry hosts.Entry) {
	key := SessionKey(entry.Alias, entry.Source)

	// Same host as the displayed session: offer to disconnect.
	if alias, source, ok := a.termPane.ActiveEntry(); ok && alias == entry.Alias && source == entry.Source {
		a.showDisconnectModal(entry)
		return
	}

	// A background session exists for this host: switch to it.
	if a.termPane.HasSession(key) {
		a.termPane.Activate(key)
		a.showTerminalPane()
		return
	}

	// New session (previous sessions keep running in the background).
	sessionID := key

	agentPipe := a.deps.AgentPipe
	if agentPipe == "" {
		agentPipe = connect.AgentPipePath()
	}

	argv, env, err := connect.CommandFor(entry, sessionID, agentPipe, a.deps.VaultCli, a.deps.Agent)
	if err != nil {
		fmt.Fprintf(os.Stderr, "wardenssh: prepare connect: %v\n", err)
		return
	}

	// Mark the host as live.
	a.hostList.MarkLive(entry.Alias, entry.Source)
	a.hostPane.Refresh()

	// Start the terminal pane (new session; old ones stay alive).
	err = a.termPane.StartSSH(entry, argv, env, func(exitErr error) {
		// Release session key from agent if loaded.
		if entry.Source != "file" && a.deps.Agent != nil {
			_ = a.deps.Agent.ReleaseSession(sessionID)
		}
		// Called from the backend read-loop goroutine — marshal UI updates.
		a.app.QueueUpdateDraw(func() {
			a.hostList.MarkDead(entry.Alias, entry.Source)
			a.hostPane.Refresh()
			if a.termPane.SessionCount() == 0 {
				a.showHostListPane()
				return
			}
			a.termPane.SyncToMostRecent()
			a.showTerminalPane()
		})
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "wardenssh: start ssh: %v\n", err)
		a.hostList.MarkDead(entry.Alias, entry.Source)
		a.hostPane.Refresh()
		return
	}

	// Switch to terminal pane.
	a.showTerminalPane()
}

func (a *App) showTerminalPane() {
	content := a.root.GetItem(0).(*tview.Flex)
	if content.GetItemCount() < 2 {
		content.AddItem(a.right, 0, 2, true)
	}
	a.FocusTerminal()
}

func (a *App) showHostListPane() {
	content := a.root.GetItem(0).(*tview.Flex)
	for content.GetItemCount() > 1 {
		content.RemoveItem(a.right)
	}
	a.FocusHostList()
}

// handleGlobalKeys intercepts keys at the app level. Input capture runs BEFORE
// the focused widget sees the event, so this handler decides what the app owns
// vs. what gets forwarded to the embedded terminal.
//
//   - Terminal pane focused (session running): only Ctrl+B and Escape are app
//     keys (move focus to the host list; the session keeps running). Every
//     other key — including 'q', Ctrl+C, and letter keys — is forwarded to the
//     terminal. Ctrl+C is cloned so tview's built-in "Ctrl+C stops the app"
//     (application.go) does not fire; the remote shell receives SIGINT.
//   - Host list focused: Escape (with empty filter) / 'q' / Ctrl+C open the
//     quit confirmation modal (Q31/C). Escape with a non-empty filter clears
//     the filter instead. Ctrl+B moves focus to the terminal when a session
//     is running.
//   - Setup / quit / disconnect / create modal: passed through so those modals handle
//     their own keys.
func (a *App) handleGlobalKeys(event *tcell.EventKey) *tcell.EventKey {
	if a.inSetup || a.inQuit || a.inDisconnect || a.inCreate {
		return event
	}

	// Terminal pane focused.
	if a.termFocused && a.termPane.IsRunning() {
		switch event.Key() {
		case tcell.KeyEscape:
			// "Move left" — back to the host list; session keeps running.
			a.FocusHostList()
			return nil
		case tcell.KeyCtrlB:
			a.FocusHostList()
			return nil
		case tcell.KeyCtrlC:
			// Ctrl+C with an active selection copies it (like a terminal
			// emulator). Otherwise clone so tview's built-in Ctrl+C quit is
			// bypassed and the terminal receives SIGINT for the remote shell.
			if a.termPane.CopyActiveSelection() {
				return nil
			}
			return tcell.NewEventKey(tcell.KeyCtrlC, 0, tcell.ModNone)
		default:
			// Forward to the terminal.
			return event
		}
	}

	// Host list (home) focused.
	if event.Key() == tcell.KeyEscape {
		// ESC with a non-empty filter clears the filter, not quit.
		if a.hostPane.FilterText() != "" {
			a.hostPane.SetFilter("")
			return nil
		}
		a.RequestQuit()
		return nil
	}
	if event.Rune() == 'q' {
		a.RequestQuit()
		return nil
	}
	if event.Key() == tcell.KeyCtrlC {
		a.RequestQuit()
		return nil
	}
	if event.Key() == tcell.KeyCtrlB {
		// Pane toggle: host <-> terminal. Without a session there is no
		// terminal pane to switch to, so Ctrl+B is a no-op (still consumed).
		if a.termPane.IsRunning() {
			a.FocusTerminal()
		}
		return nil
	}
	return event
}

