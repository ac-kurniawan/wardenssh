package tviewui

import (
	"fmt"
	"strings"
	"sync"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/ac-kurniawan/wardenssh/internal/config"
	"github.com/ac-kurniawan/wardenssh/internal/hosts"
	"github.com/ac-kurniawan/wardenssh/internal/vaultadapter"
)

// AddVaultModal is the runtime add-vault form (Ctrl+A). Unlike SetupModal it
// collects the vault identity (name/server/email) AND the master password,
// then performs async login+sync. The vault is only added on success:
// onAdded receives the authenticated source plus the config entry; the caller
// persists the config and merges the source into the live client. Esc/Cancel
// closes the modal without changes.
type AddVaultModal struct {
	existing     []config.Vault // already-configured vaults (duplicate-name check)
	customFields config.CustomFields
	hostList     *hosts.List

	form   *tview.Form
	modal  *tview.Flex
	errMsg string
	busy   bool
	done   bool

	onAdded  func(src *vaultadapter.Source, v config.Vault)
	onCancel func()

	tapp *tview.Application
	mu   sync.Mutex
}

// NewAddVaultModal builds the add-vault modal. existing are the vaults already
// configured (used to reject duplicate names up front).
func NewAddVaultModal(existing []config.Vault, cf config.CustomFields, hl *hosts.List) *AddVaultModal {
	m := &AddVaultModal{
		existing:     existing,
		customFields: cf,
		hostList:     hl,
	}
	m.buildForm()
	m.modal = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(nil, 0, 1, false).
		AddItem(m.form, 10, 0, true).
		AddItem(nil, 0, 1, false)
	return m
}

func (m *AddVaultModal) buildForm() {
	m.form = tview.NewForm()
	// Border is required: tview renders the box title (which carries errors)
	// only when the border is enabled.
	m.form.SetBorder(true)
	m.form.SetTitle(" Add Vault ")
	m.form.AddInputField("Name:", "", 32, nil, nil)
	m.form.AddInputField("Server:", "https://", 40, nil, nil)
	m.form.AddInputField("Email:", "", 40, nil, nil)
	m.form.AddPasswordField("Password:", "", 40, '*', nil)
	m.form.AddButton("Add", func() { m.Submit() })
	m.form.AddButton("Cancel", func() { m.Cancel() })
	m.form.SetCancelFunc(func() { m.Cancel() })
	m.form.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEnter && !m.busy {
			m.Submit()
			return nil
		}
		return event
	})
}

func (m *AddVaultModal) updateTitle() {
	switch {
	case m.busy:
		m.form.SetTitle(" Adding vault… ")
	case m.errMsg != "":
		m.form.SetTitle(fmt.Sprintf(" Add Vault [red](%s)[-] ", m.errMsg))
	default:
		m.form.SetTitle(" Add Vault ")
	}
}

// Primitive returns the tview primitive for layout embedding.
func (m *AddVaultModal) Primitive() tview.Primitive { return m.modal }

// SetOnAdded installs the callback fired after a successful login+sync. The
// caller owns persistence (config.AddVault) and client assembly (AddSource).
func (m *AddVaultModal) SetOnAdded(fn func(src *vaultadapter.Source, v config.Vault)) {
	m.onAdded = fn
}

// SetOnCancel installs the callback fired when the modal is cancelled.
func (m *AddVaultModal) SetOnCancel(fn func()) { m.onCancel = fn }

// SetApplication attaches the tview application so async results repaint the
// modal on the event loop (tview is not thread-safe).
func (m *AddVaultModal) SetApplication(a *tview.Application) { m.tapp = a }

func (m *AddVaultModal) field(idx int) *tview.InputField {
	if f, ok := m.form.GetFormItem(idx).(*tview.InputField); ok {
		return f
	}
	return nil
}

// Name returns the name input (for tests).
func (m *AddVaultModal) Name() string { return m.field(0).GetText() }

// Server returns the server input (for tests).
func (m *AddVaultModal) Server() string { return m.field(1).GetText() }

// Email returns the email input (for tests).
func (m *AddVaultModal) Email() string { return m.field(2).GetText() }

// Password returns the password input (for tests).
func (m *AddVaultModal) Password() string { return m.field(3).GetText() }

// SetName sets the name input (for tests).
func (m *AddVaultModal) SetName(s string) { m.field(0).SetText(s) }

// SetServer sets the server input (for tests).
func (m *AddVaultModal) SetServer(s string) { m.field(1).SetText(s) }

// SetEmail sets the email input (for tests).
func (m *AddVaultModal) SetEmail(s string) { m.field(2).SetText(s) }

// SetPassword sets the password input (for tests).
func (m *AddVaultModal) SetPassword(s string) { m.field(3).SetText(s) }

// Error returns the last error message (for tests).
func (m *AddVaultModal) Error() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.errMsg
}

// IsDone reports whether a vault was successfully added.
func (m *AddVaultModal) IsDone() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.done
}

// Cancel closes the modal without changes.
func (m *AddVaultModal) Cancel() {
	m.mu.Lock()
	cancel := m.onCancel
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// Submit validates the form, then runs async login+sync. On success the
// refresh token is stored in the OS keyring (same as setup) and onAdded fires.
// On failure the error is shown in the title and the form stays filled for
// retry — a vault that cannot be logged into or synced is never added.
func (m *AddVaultModal) Submit() {
	m.mu.Lock()
	if m.busy || m.done {
		m.mu.Unlock()
		return
	}
	name := strings.TrimSpace(m.Name())
	server := strings.TrimSpace(m.Server())
	email := strings.TrimSpace(m.Email())
	pass := m.Password()
	m.mu.Unlock()

	if err := config.ValidateVaultName(name); err != nil {
		m.fail(err.Error())
		return
	}
	for _, v := range m.existing {
		if v.Name == name {
			m.fail(fmt.Sprintf("vault %q is already configured", name))
			return
		}
	}
	if server == "" || !strings.HasPrefix(server, "http://") && !strings.HasPrefix(server, "https://") {
		m.fail("server must be an http(s) URL")
		return
	}
	if email == "" || !strings.Contains(email, "@") {
		m.fail("email is required")
		return
	}
	if pass == "" {
		m.fail("master password is required")
		return
	}

	m.mu.Lock()
	m.busy = true
	m.errMsg = ""
	m.mu.Unlock()
	m.redraw(func() {
		m.setFormLocked(true)
		m.updateTitle()
	})

	cf := m.customFields
	go func() {
		c := vaultclientNew(server)
		sess, err := c.Login(email, pass)
		if err != nil {
			m.fail(err.Error())
			return
		}
		sr, err := c.Sync(sess)
		if err != nil {
			m.fail("Signed in, but the vault could not be loaded. Try again")
			return
		}
		// Login+sync succeeded: store the refresh token (same code path as
		// setup) and hand the source + config entry to the caller.
		if sess.RefreshToken != "" {
			_ = keyringSetRefreshToken(name, sess.RefreshToken)
		}
		src := vaultadapterNewSource(name, sess, sr.Ciphers, cf)

		m.mu.Lock()
		m.busy = false
		m.done = true
		added := m.onAdded
		m.mu.Unlock()

		m.redraw(func() {
			m.setFormLocked(false)
			m.updateTitle()
		})

		if m.hostList != nil {
			if entries, err := appVaultEntries(vaultadapterNewClient(src)); err == nil {
				m.hostList.Merge(entries)
			}
		}
		if added != nil {
			added(src, config.Vault{Name: name, Server: server, Email: email})
		}
	}()
}

// fail records a user-visible error and unlocks the form for retry.
func (m *AddVaultModal) fail(msg string) {
	m.mu.Lock()
	m.busy = false
	m.errMsg = msg
	m.mu.Unlock()
	m.redraw(func() {
		m.setFormLocked(false)
		m.updateTitle()
	})
}

// ShowError records a user-visible error after the modal handed control back
// to the caller (e.g. AddSource/config persistence failed in the wiring) and
// unlocks the form for retry.
func (m *AddVaultModal) ShowError(msg string) {
	m.mu.Lock()
	m.done = false
	m.mu.Unlock()
	m.fail(msg)
}

// redraw runs fn on the tview event loop when an application is attached
// (production); without an application (tests) it runs fn inline.
func (m *AddVaultModal) redraw(fn func()) {
	if m.tapp == nil {
		fn()
		return
	}
	m.tapp.QueueUpdateDraw(fn)
}

// setFormLocked disables all inputs and buttons while a login is in flight.
func (m *AddVaultModal) setFormLocked(locked bool) {
	if m.form == nil {
		return
	}
	for i := range m.form.GetFormItemCount() {
		if f, ok := m.form.GetFormItem(i).(*tview.InputField); ok {
			f.SetDisabled(locked)
		}
	}
	for _, label := range []string{"Add", "Cancel"} {
		if idx := m.form.GetButtonIndex(label); idx >= 0 {
			m.form.GetButton(idx).SetDisabled(locked)
		}
	}
}

