package tviewui

import (
	"fmt"
	"strings"
	"sync"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// CreateParams holds user input from CreateModal.
type CreateParams struct {
	Alias     string
	Target    string // "~/.ssh/config" or "vw:<name>"
	HostName  string
	User      string
	Port      string
	ProxyJump string
	AuthKind  string // "key" or "password"
	Password  string
	KeyAlgo   string // "ed25519" or "rsa4096"
}

// CreateModal is the interactive form modal for creating a new SSH connection.
type CreateModal struct {
	form     *tview.Form
	flex     *tview.Flex
	targets  []string
	params   CreateParams
	errMsg   string
	onSubmit func(CreateParams) error
	onCancel func()
	mu       sync.Mutex
}

// NewCreateModal constructs the creation form modal.
func NewCreateModal(targets []string) *CreateModal {
	if len(targets) == 0 {
		targets = []string{"~/.ssh/config"}
	}

	m := &CreateModal{
		targets: targets,
		params: CreateParams{
			Target:   targets[0],
			Port:     "22",
			AuthKind: "key",
			KeyAlgo:  "ed25519",
		},
	}
	m.buildForm()
	m.flex = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(nil, 0, 1, false).
		AddItem(m.form, 22, 0, true).
		AddItem(nil, 0, 1, false)
	return m
}

func (m *CreateModal) buildForm() {
	m.form = tview.NewForm()
	m.updateTitle()

	m.form.AddInputField("Alias / Name:", m.params.Alias, 30, nil, func(text string) {
		m.params.Alias = text
	})

	m.form.AddDropDown("Destination:", m.targets, 0, func(option string, index int) {
		m.params.Target = option
	})

	m.form.AddInputField("Hostname / IP:", m.params.HostName, 30, nil, func(text string) {
		m.params.HostName = text
	})

	m.form.AddInputField("User:", m.params.User, 20, nil, func(text string) {
		m.params.User = text
	})

	m.form.AddInputField("Port:", m.params.Port, 10, nil, func(text string) {
		m.params.Port = text
	})

	m.form.AddInputField("ProxyJump:", m.params.ProxyJump, 30, nil, func(text string) {
		m.params.ProxyJump = text
	})

	m.form.AddDropDown("Credential:", []string{"Key (Ed25519/RSA)", "Password"}, 0, func(option string, index int) {
		if index == 1 {
			m.params.AuthKind = "password"
		} else {
			m.params.AuthKind = "key"
		}
	})

	m.form.AddPasswordField("Password (if password auth):", m.params.Password, 30, '*', func(text string) {
		m.params.Password = text
	})

	m.form.AddDropDown("Key Algo (if key auth):", []string{"Ed25519", "RSA 4096"}, 0, func(option string, index int) {
		if index == 1 {
			m.params.KeyAlgo = "rsa4096"
		} else {
			m.params.KeyAlgo = "ed25519"
		}
	})

	m.form.AddButton("Save", func() {
		m.Submit()
	})
	m.form.AddButton("Cancel", func() {
		m.triggerCancel()
	})
	m.form.SetCancelFunc(func() {
		m.triggerCancel()
	})
	m.form.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		defer m.UpdateFieldStyles()

		if event.Key() == tcell.KeyEscape {
			m.triggerCancel()
			return nil
		}

		itemIdx, _ := m.form.GetFocusedItemIndex()
		if itemIdx >= 0 && itemIdx < m.form.GetFormItemCount() {
			item := m.form.GetFormItem(itemIdx)
			if dd, ok := item.(*tview.DropDown); ok {
				if dd.IsOpen() {
					return event
				}
			}
		}

		switch event.Key() {
		case tcell.KeyDown:
			return tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone)
		case tcell.KeyUp:
			return tcell.NewEventKey(tcell.KeyBacktab, 0, tcell.ModNone)
		}
		return event
	})
	m.UpdateFieldStyles()
}

// UpdateFieldStyles updates background colors and label styles of all form fields
// so the active (focused) field is prominently highlighted with a bold yellow label
// and vibrant blue background (tcell.Color24), while unfocused fields remain subtle.
func (m *CreateModal) UpdateFieldStyles() {
	focusedItemIdx, _ := m.form.GetFocusedItemIndex()

	for i := 0; i < m.form.GetFormItemCount(); i++ {
		item := m.form.GetFormItem(i)
		isFocused := (i == focusedItemIdx)

		bgColor := tcell.Color236
		fgColor := tcell.Color255
		labelColor := tcell.Color252
		if isFocused {
			bgColor = tcell.Color24
			fgColor = tcell.Color255
			labelColor = tcell.ColorYellow
		}

		fieldStyle := tcell.StyleDefault.Background(bgColor).Foreground(fgColor)
		if isFocused {
			fieldStyle = fieldStyle.Bold(true)
		}
		labelStyle := tcell.StyleDefault.Foreground(labelColor)
		if isFocused {
			labelStyle = labelStyle.Bold(true)
		}

		switch v := item.(type) {
		case *tview.InputField:
			v.SetFieldStyle(fieldStyle)
			v.SetFieldBackgroundColor(bgColor)
			v.SetFieldTextColor(fgColor)
			v.SetLabelStyle(labelStyle)
			v.SetLabelColor(labelColor)
		case *tview.DropDown:
			v.SetFieldStyle(fieldStyle)
			v.SetFieldBackgroundColor(bgColor)
			v.SetFieldTextColor(fgColor)
			v.SetLabelStyle(labelStyle)
			v.SetLabelColor(labelColor)
			v.SetFocusedStyle(fieldStyle)
			v.SetListStyles(
				tcell.StyleDefault.Background(tcell.Color236).Foreground(tcell.Color255),
				tcell.StyleDefault.Background(tcell.Color24).Foreground(tcell.ColorYellow).Bold(true),
			)
		}
	}

	m.form.SetButtonActivatedStyle(tcell.StyleDefault.Background(tcell.Color24).Foreground(tcell.ColorYellow).Bold(true))
	m.form.SetButtonStyle(tcell.StyleDefault.Background(tcell.Color236).Foreground(tcell.Color255))
}

func (m *CreateModal) updateTitle() {
	if m.errMsg != "" {
		m.form.SetTitle(fmt.Sprintf(" Create New Connection [red](%s)[-] ", m.errMsg))
	} else {
		m.form.SetTitle(" Create New SSH Connection ")
	}
}

// Form returns the underlying tview.Form primitive.
func (m *CreateModal) Form() *tview.Form { return m.form }

// Primitive returns the tview primitive for layout embedding.
func (m *CreateModal) Primitive() tview.Primitive { return m.flex }

// SetOnSubmit installs the submission callback.
func (m *CreateModal) SetOnSubmit(fn func(CreateParams) error) { m.onSubmit = fn }

// SetOnCancel installs the cancel callback.
func (m *CreateModal) SetOnCancel(fn func()) { m.onCancel = fn }

// Error returns the last error message recorded on the modal.
func (m *CreateModal) Error() string { return m.errMsg }

// Setters for programmatic manipulation and testing.
func (m *CreateModal) SetAlias(a string)     { m.params.Alias = a }
func (m *CreateModal) SetHostName(h string)  { m.params.HostName = h }
func (m *CreateModal) SetUser(u string)      { m.params.User = u }
func (m *CreateModal) SetPort(p string)      { m.params.Port = p }
func (m *CreateModal) SetProxyJump(pj string){ m.params.ProxyJump = pj }
func (m *CreateModal) SetAuthKind(k string)  { m.params.AuthKind = k }
func (m *CreateModal) SetPassword(p string)  { m.params.Password = p }
func (m *CreateModal) SetKeyAlgo(k string)   { m.params.KeyAlgo = k }
func (m *CreateModal) SetTarget(t string)    { m.params.Target = t }

// TriggerCancel fires the cancel callback.
func (m *CreateModal) TriggerCancel() { m.triggerCancel() }

func (m *CreateModal) triggerCancel() {
	if m.onCancel != nil {
		m.onCancel()
	}
}

// Submit validates inputs and triggers the onSubmit callback.
func (m *CreateModal) Submit() {
	m.params.Alias = strings.TrimSpace(m.params.Alias)
	m.params.HostName = strings.TrimSpace(m.params.HostName)
	m.params.User = strings.TrimSpace(m.params.User)
	m.params.Port = strings.TrimSpace(m.params.Port)
	m.params.ProxyJump = strings.TrimSpace(m.params.ProxyJump)

	if m.params.Alias == "" {
		m.errMsg = "Alias / Name is required"
		m.updateTitle()
		return
	}
	if m.params.HostName == "" {
		m.errMsg = "Hostname / IP is required"
		m.updateTitle()
		return
	}
	if m.params.AuthKind == "password" && strings.TrimSpace(m.params.Password) == "" {
		m.errMsg = "Password is required for password credential"
		m.updateTitle()
		return
	}

	m.errMsg = ""
	m.updateTitle()

	if m.onSubmit != nil {
		if err := m.onSubmit(m.params); err != nil {
			m.errMsg = err.Error()
			m.updateTitle()
		}
	}
}
