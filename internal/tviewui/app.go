package tviewui

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/ac-kurniawan/wardenssh/internal/config"
	"github.com/ac-kurniawan/wardenssh/internal/connect"
	"github.com/ac-kurniawan/wardenssh/internal/hosts"
	"github.com/ac-kurniawan/wardenssh/internal/session"
	"github.com/ac-kurniawan/wardenssh/internal/sshagent"
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
}

// App is the main WardenSSH launcher TUI application.
type App struct {
	app      *tview.Application
	hostList *hosts.List
	deps     Deps
	vaults   []config.Vault

	hostPane   *HostListPane
	termPane   *TerminalPane
	setupModal *SetupModal
	quitModal  *QuitModal

	root    *tview.Flex
	left    *tview.Flex
	right   *tview.Flex
	overlay *tview.Pages

	mu          sync.Mutex
	inSetup     bool
	inQuit      bool
	syncStarted bool
	syncTicker  *time.Ticker
	stopSync    chan struct{}
}

// New creates the TUI app. If vaults is non-empty, it starts in setup mode.
func New(hostList *hosts.List, deps Deps, vaults []config.Vault) *App {
	a := &App{
		app:      tview.NewApplication(),
		hostList: hostList,
		deps:     deps,
		vaults:   vaults,
	}

	// Build panes.
	a.hostPane = NewHostListPane(hostList)
	a.termPane = NewTerminalPane(a.app)
	a.hostPane.Refresh()

	// Wire host pane callbacks.
	a.hostPane.SetOnConnect(a.handleConnect)
	a.hostPane.SetOnScopeChange(func() {})
	a.hostPane.SetOnRefresh(func() { _ = a.TriggerSync() })

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

	// Pages for modal overlays (setup, quit).
	a.overlay = tview.NewPages().
		AddPage("main", a.root, true, true)
	a.app.SetRoot(a.overlay, true)

	// Global key handling.
	a.app.SetInputCapture(a.handleGlobalKeys)

	// Setup mode?
	if len(vaults) > 0 {
		a.inSetup = true
		a.setupModal = NewSetupModal(vaults, deps.CustomFields, hostList)
		a.setupModal.SetOnComplete(func(vc vault.Client) {
			a.inSetup = false
			a.deps.VaultCli = vc
			a.overlay.RemovePage("setup")
			a.hostPane.Refresh()
			a.app.SetFocus(a.hostPane.Primitive())
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

// RequestQuit returns true if the app should quit immediately (no live
// sessions), or false if it opened the quit modal (live sessions exist).
func (a *App) RequestQuit() bool {
	if a.hasLiveSessions() {
		a.showQuitModal()
		return false
	}
	a.app.Stop()
	return true
}

func (a *App) hasLiveSessions() bool {
	for _, e := range a.hostList.All() {
		if e.Live {
			return true
		}
	}
	return false
}

func (a *App) showQuitModal() {
	a.inQuit = true
	a.quitModal = NewQuitModal()
	a.quitModal.SetOnKillAll(func() {
		if a.deps.Mgr != nil {
			a.deps.Mgr.KillAll()
		}
		a.clearAllLive()
		a.app.Stop()
	})
	a.quitModal.SetOnDetach(func() {
		a.app.Stop()
	})
	a.quitModal.SetOnCancel(func() {
		a.inQuit = false
		a.overlay.RemovePage("quit")
		a.app.SetFocus(a.hostPane.Primitive())
	})
	a.overlay.AddPage("quit", a.quitModal.Primitive(), true, true)
	a.app.SetFocus(a.quitModal.Primitive())
}

func (a *App) clearAllLive() {
	for _, e := range a.hostList.All() {
		if e.Live {
			a.hostList.MarkDead(e.Alias, e.Source)
		}
	}
}

// handleConnect is called when Enter is pressed on a host entry.
func (a *App) handleConnect(entry hosts.Entry) {
	sessionID := fmt.Sprintf("%s-%s", entry.Alias, entry.Source)

	// Decrypt vault key + load into agent if vault-sourced (Q8/C, Q19/B).
	if entry.Source != "file" && a.deps.VaultCli != nil && a.deps.Agent != nil {
		if err := connect.PrepareAgentKey(entry, sessionID, a.deps.VaultCli, a.deps.Agent); err != nil {
			fmt.Fprintf(os.Stderr, "wardenssh: prepare key: %v\n", err)
			return
		}
	}

	agentPipe := a.deps.AgentPipe
	if agentPipe == "" {
		agentPipe = connect.AgentPipePath()
	}
	argv := connect.SSHArgv(entry, agentPipe)
	env := connect.EnvForAgent(agentPipe)

	// Mark the host as live.
	a.hostList.MarkLive(entry.Alias, entry.Source)
	a.hostPane.Refresh()

	// Start the terminal pane.
	err := a.termPane.StartSSH(entry, argv, env, func(exitErr error) {
		// Release session key from agent if loaded.
		if entry.Source != "file" && a.deps.Agent != nil {
			_ = a.deps.Agent.ReleaseSession(sessionID)
		}
		// Called when ssh exits — return to host list.
		a.app.QueueUpdateDraw(func() {
			a.hostList.MarkDead(entry.Alias, entry.Source)
			a.hostPane.Refresh()
			a.showHostListPane()
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
	content.AddItem(a.right, 0, 2, true)
	a.app.SetFocus(a.termPane.Primitive())
}

func (a *App) showHostListPane() {
	content := a.root.GetItem(0).(*tview.Flex)
	for content.GetItemCount() > 1 {
		content.RemoveItem(a.right)
	}
	a.termPane.Close()
	a.app.SetFocus(a.hostPane.Primitive())
}

// handleGlobalKeys intercepts keys at the app level.
func (a *App) handleGlobalKeys(event *tcell.EventKey) *tcell.EventKey {
	if a.inSetup || a.inQuit {
		return event
	}
	if event.Rune() == 'q' {
		a.RequestQuit()
		return nil
	}
	if event.Key() == tcell.KeyCtrlC {
		a.RequestQuit()
		return nil
	}
	return event
}

