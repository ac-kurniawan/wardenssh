// Package tui is the WardenSSH launcher Bubble Tea program (Q10/C host list,
// Q29/B fuzzy filter + scope cycle, Q18/iii live-session green dots, Q31/C
// quit-confirmation modal intercepting SIGINT). The model owns a hosts.List
// and translates keystrokes into list/filter/scope state and connect/quit
// intents; the real session-manager wiring (PTY, suspend-and-exec) plugs in
// via ConnectMsg (Subsystem: PTY session manager, tested separately).
package tui

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/ac-kurniawan/wardenssh/internal/config"
	"github.com/ac-kurniawan/wardenssh/internal/connect"
	"github.com/ac-kurniawan/wardenssh/internal/hosts"
	"github.com/ac-kurniawan/wardenssh/internal/session"
	"github.com/ac-kurniawan/wardenssh/internal/sshagent"
	"github.com/ac-kurniawan/wardenssh/internal/vault"
	"github.com/ac-kurniawan/wardenssh/internal/vaultadapter"
	"github.com/ac-kurniawan/wardenssh/internal/vaultclient"
)

// Indirection points for testability (tests can override these to inject
// mock vault clients without touching real network endpoints).
var (
	vaultclientNew     = vaultclient.New
	vaultadapterNewClient = vaultadapter.NewClient
	vaultadapterNewSource = vaultadapter.NewSource
)

// ConnectMsg is emitted by Enter on a selected entry, carrying the entry the
// session manager should open.
type ConnectMsg struct {
	Entry hosts.Entry
}

// SessionExitedMsg is emitted when a spawned session exits.
type SessionExitedMsg struct {
	Alias     string
	Source    string
	SessionID string
}

// VaultReadyMsg is emitted when a vault login + sync succeeds. The assembled
// vault.Source is carried for the model to merge into its vault client.
type VaultReadyMsg struct {
	Source *vaultadapter.Source
}

// VaultErrorMsg is emitted when a vault login or sync fails. The model stays
// in setup state, displays the error, and clears the password for retry.
type VaultErrorMsg struct {
	Err error
}

// Connect intent — handled by the model or test.
func newConnectCmd(e hosts.Entry) tea.Cmd {
	return func() tea.Msg { return ConnectMsg{Entry: e} }
}

type state int

const (
	stateList state = iota
	stateQuitModal
	stateSetup
)

// Deps holds injected dependencies for the TUI model.
type Deps struct {
	Agent        *sshagent.Keyring
	Mgr          *session.Manager
	VaultCli     vault.Client
	AgentPipe    string
	CustomFields config.CustomFields
}

// Model is the Bubble Tea model for the launcher. It is a value type with a
// pointer-to-List so filter/scope mutations are visible across copies.
type Model struct {
	hostList   *hosts.List
	filter     string
	cursor     int
	st         state
	lastAction string
	errStatus  string

	agent        *sshagent.Keyring
	mgr          *session.Manager
	vaultCli     vault.Client
	agentPipe    string
	customFields config.CustomFields

	// Setup modal state
	setupInput     textinput.Model
	setupVaults    []config.Vault
	setupVaultIdx  int
	setupError     string
	setupSources   []*vaultadapter.Source
	setupLoggingIn bool
}

// New returns the initial launcher model over the given host list.
func New(h *hosts.List) Model {
	return Model{hostList: h, st: stateList}
}

// NewWithDeps returns the launcher model with injected session/agent dependencies.
func NewWithDeps(h *hosts.List, deps Deps) Model {
	return Model{
		hostList:     h,
		st:           stateList,
		agent:        deps.Agent,
		mgr:          deps.Mgr,
		vaultCli:     deps.VaultCli,
		agentPipe:    deps.AgentPipe,
		customFields: deps.CustomFields,
	}
}

// NewWithSetup returns a model that starts in the setup (vault unlock) state.
// The user is prompted sequentially for each vault's master password. Esc
// skips the current vault; after all vaults are processed (or skipped), the
// model transitions to the list state.
func NewWithSetup(h *hosts.List, deps Deps, vaults []config.Vault) Model {
	ti := textinput.New()
	ti.EchoMode = textinput.EchoPassword
	ti.EchoCharacter = '*'
	ti.Placeholder = "master password"
	ti.Focus()
	ti.CharLimit = 0
	return Model{
		hostList:      h,
		st:            stateSetup,
		agent:         deps.Agent,
		mgr:           deps.Mgr,
		agentPipe:     deps.AgentPipe,
		customFields:  deps.CustomFields,
		setupInput:    ti,
		setupVaults:   vaults,
		setupVaultIdx: 0,
	}
}

// SyncTickMsg triggers a background vault sync check.
type SyncTickMsg struct{}

// SyncResultMsg carries the outcome of a background vault sync attempt.
type SyncResultMsg struct {
	VaultName string
	Err       error
}

func syncTickCmd() tea.Cmd {
	return tea.Tick(5*time.Minute, func(time.Time) tea.Msg {
		return SyncTickMsg{}
	})
}

// Init satisfies tea.Model.
func (m Model) Init() tea.Cmd {
	if m.st == stateSetup {
		return textinput.Blink // cursor blink for the password field
	}
	if m.vaultCli != nil {
		return syncTickCmd()
	}
	return nil
}

// Update satisfies tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case VaultReadyMsg:
		return m.handleVaultReady(msg)
	case VaultErrorMsg:
		m.setupError = msg.Err.Error()
		m.setupInput.Reset()
		m.setupLoggingIn = false
		return m, nil
	case ConnectMsg:
		if m.mgr == nil || m.agent == nil {
			// No deps injected (e.g. basic unit test); intent was emitted.
			return m, nil
		}
		sessID := generateSessionID()
		res := connect.Connect(msg.Entry, sessID, m.vaultCli, &connect.Connector{
			Agent: m.agent,
			Mgr:   m.mgr,
		})
		if res.Err != nil {
			m.errStatus = res.Err.Error()
			return m, nil
		}
		m.hostList.MarkLive(msg.Entry.Alias, msg.Entry.Source)
		sess := res.Session
		return m, func() tea.Msg {
			<-sess.Done()
			return SessionExitedMsg{
				Alias:     msg.Entry.Alias,
				Source:    msg.Entry.Source,
				SessionID: sessID,
			}
		}
	case SessionExitedMsg:
		if m.agent != nil {
			m.agent.ReleaseSession(msg.SessionID)
		}
		m.hostList.MarkDead(msg.Alias, msg.Source)
		return m, nil
	case SyncTickMsg:
		if m.vaultCli == nil {
			return m, nil
		}
		cli := m.vaultCli
		return m, tea.Batch(
			func() tea.Msg {
				err := cli.Sync()
				return SyncResultMsg{VaultName: "all", Err: err}
			},
			syncTickCmd(),
		)
	case SyncResultMsg:
		if msg.Err != nil {
			m.errStatus = "sync fail: " + msg.Err.Error()
		}
		return m, nil
	case tea.KeyMsg:
		switch m.st {
		case stateSetup:
			return m.updateSetup(msg)
		case stateList:
			return m.updateList(msg)
		case stateQuitModal:
			return m.updateQuitModal(msg)
		}
	}
	return m, nil
}

func generateSessionID() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// --- setup state ---

// updateSetup handles keystrokes while in the vault unlock modal.
func (m Model) updateSetup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// While a login is in-flight, ignore all keystrokes.
	if m.setupLoggingIn {
		return m, nil
	}
	switch msg.Type {
	case tea.KeyEsc:
		return m.skipCurrentVault()
	case tea.KeyEnter:
		pass := m.setupInput.Value()
		if pass == "" {
			return m, nil
		}
		m.setupLoggingIn = true
		m.setupError = ""
		v := m.setupVaults[m.setupVaultIdx]
		cf := m.customFields
		return m, loginCmd(v, pass, cf)
	case tea.KeyBackspace:
		var cmd tea.Cmd
		m.setupInput, cmd = m.setupInput.Update(msg)
		return m, cmd
	case tea.KeyRunes, tea.KeyCtrlU, tea.KeyCtrlW:
		var cmd tea.Cmd
		m.setupInput, cmd = m.setupInput.Update(msg)
		return m, cmd
	default:
		return m, nil
	}
}

// skipCurrentVault advances to the next vault or transitions to list state
// if all vaults have been processed.
func (m Model) skipCurrentVault() (tea.Model, tea.Cmd) {
	m.setupVaultIdx++
	m.setupError = ""
	m.setupInput.Reset()
	if m.setupVaultIdx >= len(m.setupVaults) {
		return m.transitionToList()
	}
	return m, nil
}

// handleVaultReady collects the successful source and either advances to the
// next vault prompt or transitions to the list state with the merged client.
func (m Model) handleVaultReady(msg VaultReadyMsg) (tea.Model, tea.Cmd) {
	m.setupLoggingIn = false
	if msg.Source != nil {
		m.setupSources = append(m.setupSources, msg.Source)
	}
	m.setupVaultIdx++
	m.setupError = ""
	m.setupInput.Reset()
	if m.setupVaultIdx >= len(m.setupVaults) {
		return m.transitionToList()
	}
	return m, nil
}

// transitionToList finalizes the vault client from collected sources and
// transitions to the list state.
func (m Model) transitionToList() (tea.Model, tea.Cmd) {
	if len(m.setupSources) > 0 {
		m.vaultCli = vaultadapterNewClient(m.setupSources...)
	}
	m.st = stateList
	m.setupInput.Blur()
	var cmds []tea.Cmd
	if m.vaultCli != nil {
		cmds = append(cmds, syncTickCmd())
	}
	return m, tea.Batch(cmds...)
}

// loginCmd performs the async vault login + sync. Returns VaultReadyMsg on
// success or VaultErrorMsg on failure.
func loginCmd(v config.Vault, pass string, cf config.CustomFields) tea.Cmd {
	return func() tea.Msg {
		c := vaultclientNew(v.Server)
		sess, err := c.Login(v.Email, pass)
		if err != nil {
			return VaultErrorMsg{Err: fmt.Errorf("login %q: %w", v.Name, err)}
		}
		sr, err := c.Sync(sess)
		if err != nil {
			return VaultErrorMsg{Err: fmt.Errorf("sync %q: %w", v.Name, err)}
		}
		src := vaultadapterNewSource(v.Name, sess, sr.Ciphers, cf)
		return VaultReadyMsg{Source: src}
	}
}

// View satisfies tea.Model. The real rendering (Lip Gloss styling + badges +
// green dots) is a future commit; this minimal view is enough to wire the
// program and assert state in tests.
func (m Model) View() string {
	return renderView(m)
}

// --- accessors used by tests / the future renderer ---

// List returns the underlying host list (for scope, visible entries, etc.).
func (m Model) List() *hosts.List { return m.hostList }

// Filter returns the current filter string.
func (m Model) Filter() string { return m.filter }

// InQuitModal reports whether the quit-confirmation modal is open.
func (m Model) InQuitModal() bool { return m.st == stateQuitModal }

// LastAction returns the last modal choice ("killall"/"detach"/"cancel") for
// inspection/testing; "" before any modal action.
func (m Model) LastAction() string { return m.lastAction }

// --- setup state accessors (used by tests + view renderer) ---

// InSetup reports whether the vault unlock modal is active.
func (m Model) InSetup() bool { return m.st == stateSetup }

// SetupPrompt returns the prompt text for the current vault being unlocked.
func (m Model) SetupPrompt() string {
	if m.setupVaultIdx >= len(m.setupVaults) {
		return ""
	}
	v := m.setupVaults[m.setupVaultIdx]
	return fmt.Sprintf("%s (%s)", v.Name, v.Email)
}

// SetupPassword returns the current password input value (for tests).
func (m Model) SetupPassword() string { return m.setupInput.Value() }

// SetupError returns the last login error message (empty if none).
func (m Model) SetupError() string { return m.setupError }

// VaultClient returns the assembled vault client (nil until setup completes).
func (m Model) VaultClient() vault.Client { return m.vaultCli }

// SetupInputView returns the textinput view string (masked password field).
func (m Model) SetupInputView() string { return m.setupInput.View() }

// --- list state ---

func (m Model) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.Type == tea.KeyCtrlC:
		return m.requestQuit()
	case msg.Type == tea.KeyTab:
		m.hostList.Tab()
		m.cursor = 0
		return m, nil
	case msg.Type == tea.KeyUp:
		if m.cursor > 0 {
			m.cursor--
		}
		return m, nil
	case msg.Type == tea.KeyDown:
		vis := m.hostList.Visible()
		if m.cursor < len(vis)-1 {
			m.cursor++
		}
		return m, nil
	case msg.Type == tea.KeyEnter:
		vis := m.hostList.Visible()
		if len(vis) == 0 {
			return m, nil
		}
		idx := m.cursor
		if idx < 0 || idx >= len(vis) {
			idx = 0
		}
		return m, newConnectCmd(vis[idx])
	case msg.Type == tea.KeyBackspace:
		if len(m.filter) > 0 {
			m.filter = m.filter[:len(m.filter)-1]
			m.hostList.SetFilter(m.filter)
			m.cursor = 0
		}
		return m, nil
	case msg.Type == tea.KeyRunes:
		s := string(msg.Runes)
		if s == "q" {
			return m.requestQuit()
		}
		m.filter += s
		m.hostList.SetFilter(m.filter)
		m.cursor = 0
		return m, nil
	}
	return m, nil
}

// requestQuit implements Q31/C: 'q'/Ctrl+C quit immediately when no live
// sessions exist; otherwise open the confirmation modal.
func (m Model) requestQuit() (tea.Model, tea.Cmd) {
	if m.hasLiveSessions() {
		m.st = stateQuitModal
		return m, nil
	}
	m.lastAction = "killall" // no sessions to kill; clean quit
	return m, tea.Quit
}

func (m Model) hasLiveSessions() bool {
	for _, e := range m.hostList.All() {
		if e.Live {
			return true
		}
	}
	return false
}

// --- quit modal state ---

func (m Model) updateQuitModal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.Type == tea.KeyRunes:
		switch string(msg.Runes) {
		case "k", "y":
			m.lastAction = "killall"
			m.clearAllLive()
			return m, tea.Quit
		case "d":
			m.lastAction = "detach"
			// Detach semantics (leave sessions agentless) live in the session
			// manager; the model just signals quit.
			return m, tea.Quit
		case "c":
			m.lastAction = "cancel"
			m.st = stateList
			return m, nil
		}
	case msg.Type == tea.KeyEsc:
		m.lastAction = "cancel"
		m.st = stateList
		return m, nil
	case msg.Type == tea.KeyEnter:
		// Default action is Kill all.
		m.lastAction = "killall"
		m.clearAllLive()
		return m, tea.Quit
	}
	return m, nil
}

// clearAllLive clears the green-dot flag on every entry (kill all sessions).
func (m Model) clearAllLive() {
	for _, e := range m.hostList.All() {
		if e.Live {
			m.hostList.MarkDead(e.Alias, e.Source)
		}
	}
}