package tviewui

import (
	"fmt"
	"sync"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/ac-kurniawan/wardenssh/internal/app"
	"github.com/ac-kurniawan/wardenssh/internal/config"
	"github.com/ac-kurniawan/wardenssh/internal/hosts"
	"github.com/ac-kurniawan/wardenssh/internal/keyring"
	"github.com/ac-kurniawan/wardenssh/internal/vault"
	"github.com/ac-kurniawan/wardenssh/internal/vaultadapter"
	"github.com/ac-kurniawan/wardenssh/internal/vaultclient"
)

// Indirection points for testability.
var (
	vaultclientNew         = vaultclient.New
	vaultadapterNewClient  = vaultadapter.NewClient
	vaultadapterNewSource  = vaultadapter.NewSource
	appVaultEntries        = app.VaultEntries
	keyringSetRefreshToken = keyring.SetRefreshToken
	keyringGetRefreshToken = keyring.GetRefreshToken
)

// SetAppVaultEntriesForTest overrides the vault-entries extractor (tests only).
func SetAppVaultEntriesForTest(f func(vault.Client) ([]hosts.Entry, error)) {
	appVaultEntries = f
}

// ResetAppVaultEntriesForTest restores the real vault-entries extractor.
func ResetAppVaultEntriesForTest() {
	appVaultEntries = app.VaultEntries
}

// SetKeyringSetRefreshTokenForTest overrides keyring.SetRefreshToken (tests only).
func SetKeyringSetRefreshTokenForTest(f func(string, string) error) {
	keyringSetRefreshToken = f
}

// ResetKeyringSetRefreshTokenForTest restores the real keyring.SetRefreshToken.
func ResetKeyringSetRefreshTokenForTest() {
	keyringSetRefreshToken = keyring.SetRefreshToken
}

// SetKeyringGetRefreshTokenForTest overrides keyring.GetRefreshToken (tests only).
func SetKeyringGetRefreshTokenForTest(f func(string) (string, error)) {
	keyringGetRefreshToken = f
}

// ResetKeyringGetRefreshTokenForTest restores the real keyring.GetRefreshToken.
func ResetKeyringGetRefreshTokenForTest() {
	keyringGetRefreshToken = keyring.GetRefreshToken
}

// SetupModal is the vault unlock modal. It prompts for the master password
// for each configured vault sequentially, performs async login+sync, and
// calls OnComplete with the assembled vault.Client when all vaults are done.
// Esc skips the current vault (graceful degradation — file-only hosts).
type SetupModal struct {
	vaults       []config.Vault
	customFields config.CustomFields
	hostList     *hosts.List

	form     *tview.Form
	modal    *tview.Flex
	password string
	errMsg   string

	idx       int
	done      bool
	loggingIn bool

	sources []*vaultadapter.Source

	onComplete func(vault.Client)
	onSkip     func()

	noKeyring bool

	mu sync.Mutex
}

// NewSetupModal builds the vault unlock modal.
func NewSetupModal(vaults []config.Vault, cf config.CustomFields, hl *hosts.List, noKeyring ...bool) *SetupModal {
	nk := false
	if len(noKeyring) > 0 {
		nk = noKeyring[0]
	}
	m := &SetupModal{
		vaults:       vaults,
		customFields: cf,
		hostList:     hl,
		noKeyring:    nk,
	}
	m.buildForm()
	m.modal = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(nil, 0, 1, false).
		AddItem(m.form, 9, 0, true).
		AddItem(nil, 0, 1, false)
	if !m.noKeyring {
		m.TryAutoLogin()
	}
	return m
}

// TryAutoLogin attempts auto-login using a stored refresh token from the OS keyring for the current vault.
// If a valid token is present and refresh succeeds, it syncs the vault and advances setup.
// If the token is missing, invalid, or refresh fails, it silently falls back to master password entry.
func (m *SetupModal) TryAutoLogin() {
	m.mu.Lock()
	if m.noKeyring || m.loggingIn || m.idx >= len(m.vaults) {
		m.mu.Unlock()
		return
	}
	v := m.vaults[m.idx]
	cf := m.customFields
	m.mu.Unlock()

	tok, err := keyringGetRefreshToken(v.Name)
	if err != nil || tok == "" {
		return
	}

	m.mu.Lock()
	if m.loggingIn || m.idx >= len(m.vaults) {
		m.mu.Unlock()
		return
	}
	m.loggingIn = true
	m.mu.Unlock()

	go func() {
		c := vaultclientNew(v.Server)
		sess, err := c.RefreshToken(tok)
		if err != nil {
			m.mu.Lock()
			m.loggingIn = false
			m.mu.Unlock()
			return
		}

		if sess.RefreshToken != "" && sess.RefreshToken != tok {
			_ = keyringSetRefreshToken(v.Name, sess.RefreshToken)
		}

		sr, err := c.Sync(sess)
		if err != nil {
			m.mu.Lock()
			m.loggingIn = false
			m.mu.Unlock()
			return
		}

		src := vaultadapterNewSource(v.Name, sess, sr.Ciphers, cf)
		m.mu.Lock()
		m.loggingIn = false
		m.sources = append(m.sources, src)
		m.idx++
		m.password = ""
		m.updateTitle()
		m.mu.Unlock()

		m.checkDone()

		m.mu.Lock()
		done := m.idx >= len(m.vaults)
		m.mu.Unlock()

		if !done {
			m.TryAutoLogin()
		}
	}()
}

func (m *SetupModal) buildForm() {
	m.form = tview.NewForm()
	m.updateTitle()
	passField := tview.NewInputField().
		SetLabel("Password:").
		SetFieldWidth(40).
		SetMaskCharacter('*').
		SetChangedFunc(func(text string) {
			m.password = text
		}).
		SetDoneFunc(func(key tcell.Key) {
			if key == tcell.KeyEnter {
				m.Submit()
			}
		})
	m.form.AddFormItem(passField)
	m.form.AddButton("Unlock", func() {
		m.Submit()
	})
	m.form.AddButton("Skip", func() {
		m.SkipCurrent()
	})
	m.form.SetCancelFunc(func() {
		m.SkipCurrent()
	})
}

func (m *SetupModal) updateTitle() {
	prompt := m.CurrentPrompt()
	if prompt == "" {
		m.form.SetTitle(" Vault Unlock ")
		return
	}
	if m.errMsg != "" {
		m.form.SetTitle(fmt.Sprintf(" Unlock Vault: %s [red](%s)[-] ", prompt, m.errMsg))
	} else {
		m.form.SetTitle(fmt.Sprintf(" Unlock Vault: %s ", prompt))
	}
}

// Primitive returns the tview primitive for layout embedding.
func (m *SetupModal) Primitive() tview.Primitive { return m.modal }

// SetOnComplete installs the callback fired when all vaults are unlocked.
func (m *SetupModal) SetOnComplete(fn func(vault.Client)) { m.onComplete = fn }

// SetOnSkip installs the callback fired when all vaults are skipped.
func (m *SetupModal) SetOnSkip(fn func()) { m.onSkip = fn }

// CurrentPrompt returns the current vault name + email.
func (m *SetupModal) CurrentPrompt() string {
	if m.idx >= len(m.vaults) {
		return ""
	}
	v := m.vaults[m.idx]
	return fmt.Sprintf("%s (%s)", v.Name, v.Email)
}

// Password returns the current password input (for tests).
func (m *SetupModal) Password() string { return m.password }

// Error returns the last error message (for tests).
func (m *SetupModal) Error() string { return m.errMsg }

// IsDone reports whether all vaults have been processed.
func (m *SetupModal) IsDone() bool { return m.done }

// TypeRune simulates typing a rune (for tests).
func (m *SetupModal) TypeRune(r rune) {
	m.password += string(r)
}

// Backspace deletes the last password character (for tests).
func (m *SetupModal) Backspace() {
	if len(m.password) > 0 {
		m.password = m.password[:len(m.password)-1]
	}
}

// Submit triggers the login flow for the current vault.
func (m *SetupModal) Submit() {
	if m.loggingIn || m.idx >= len(m.vaults) {
		return
	}
	pass := m.password
	if pass == "" {
		return
	}
	m.loggingIn = true
	m.errMsg = ""
	v := m.vaults[m.idx]
	cf := m.customFields

	go func() {
		c := vaultclientNew(v.Server)
		sess, err := c.Login(v.Email, pass)
		if err != nil {
			m.mu.Lock()
			m.loggingIn = false
			m.errMsg = fmt.Sprintf("login %q: %v", v.Name, err)
			m.password = ""
			m.updateTitle()
			m.mu.Unlock()
			return
		}
		if sess.RefreshToken != "" {
			_ = keyringSetRefreshToken(v.Name, sess.RefreshToken)
		}
		sr, err := c.Sync(sess)
		if err != nil {
			m.mu.Lock()
			m.loggingIn = false
			m.errMsg = fmt.Sprintf("sync %q: %v", v.Name, err)
			m.password = ""
			m.updateTitle()
			m.mu.Unlock()
			return
		}
		src := vaultadapterNewSource(v.Name, sess, sr.Ciphers, cf)
		m.mu.Lock()
		m.loggingIn = false
		m.sources = append(m.sources, src)
		m.idx++
		m.password = ""
		m.updateTitle()
		m.mu.Unlock()
		m.checkDone()
	}()
}

// SkipCurrent skips the current vault and advances to the next.
func (m *SetupModal) SkipCurrent() {
	m.mu.Lock()
	m.idx++
	m.password = ""
	m.errMsg = ""
	m.updateTitle()
	m.mu.Unlock()
	m.checkDone()
}

func (m *SetupModal) checkDone() {
	m.mu.Lock()
	done := m.idx >= len(m.vaults)
	sources := m.sources
	m.mu.Unlock()

	if done {
		m.done = true
		if len(sources) > 0 {
			vc := vaultadapterNewClient(sources...)
			if m.hostList != nil {
				if vEntries, err := appVaultEntries(vc); err == nil {
					m.hostList.Merge(vEntries)
				}
			}
			if m.onComplete != nil {
				m.onComplete(vc)
			}
		} else {
			if m.onSkip != nil {
				m.onSkip()
			}
		}
	}
}
