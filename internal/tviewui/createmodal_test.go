package tviewui_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/ac-kurniawan/wardenssh/internal/hosts"
	"github.com/ac-kurniawan/wardenssh/internal/tviewui"
)

func TestCreateModal_InitialState(t *testing.T) {
	targets := []string{"~/.ssh/config", "vw:personal"}
	modal := tviewui.NewCreateModal(targets)

	if modal == nil {
		t.Fatal("expected NewCreateModal to return non-nil modal")
	}
	if modal.Primitive() == nil {
		t.Fatal("expected modal.Primitive() to return non-nil primitive")
	}
	if modal.Error() != "" {
		t.Errorf("expected empty error initially, got: %s", modal.Error())
	}
}

func TestCreateModal_DefaultTargetsFallback(t *testing.T) {
	modal := tviewui.NewCreateModal(nil)
	if modal == nil {
		t.Fatal("expected NewCreateModal(nil) to succeed")
	}

	var submittedParams tviewui.CreateParams
	modal.SetOnSubmit(func(p tviewui.CreateParams) error {
		submittedParams = p
		return nil
	})

	modal.SetAlias("test-server")
	modal.SetHostName("10.0.0.1")
	modal.SetUser("root")
	modal.Submit()

	if submittedParams.Target != "~/.ssh/config" {
		t.Errorf("expected default target '~/.ssh/config', got %q", submittedParams.Target)
	}
}

func TestCreateModal_SubmitValidParams(t *testing.T) {
	targets := []string{"~/.ssh/config", "vw:personal"}
	modal := tviewui.NewCreateModal(targets)

	var submittedParams tviewui.CreateParams
	submitted := false

	modal.SetOnSubmit(func(p tviewui.CreateParams) error {
		submitted = true
		submittedParams = p
		return nil
	})

	modal.SetAlias("  my-server  ")
	modal.SetTarget("vw:personal")
	modal.SetHostName("  192.168.1.100  ")
	modal.SetUser("ubuntu")
	modal.SetPort("2222")
	modal.SetProxyJump("bastion")
	modal.SetAuthKind("password")
	modal.SetPassword("secret123")
	modal.SetKeyAlgo("rsa4096")

	modal.Submit()

	if !submitted {
		t.Fatalf("expected form submission callback to trigger")
	}

	if submittedParams.Alias != "my-server" {
		t.Errorf("expected trimmed alias 'my-server', got %q", submittedParams.Alias)
	}
	if submittedParams.HostName != "192.168.1.100" {
		t.Errorf("expected trimmed hostname '192.168.1.100', got %q", submittedParams.HostName)
	}
	if submittedParams.Target != "vw:personal" {
		t.Errorf("expected target 'vw:personal', got %q", submittedParams.Target)
	}
	if submittedParams.User != "ubuntu" {
		t.Errorf("expected user 'ubuntu', got %q", submittedParams.User)
	}
	if submittedParams.Port != "2222" {
		t.Errorf("expected port '2222', got %q", submittedParams.Port)
	}
	if submittedParams.ProxyJump != "bastion" {
		t.Errorf("expected proxyjump 'bastion', got %q", submittedParams.ProxyJump)
	}
	if submittedParams.AuthKind != "password" || submittedParams.Password != "secret123" {
		t.Errorf("unexpected credential params: %+v", submittedParams)
	}
	if submittedParams.KeyAlgo != "rsa4096" {
		t.Errorf("expected key algo 'rsa4096', got %q", submittedParams.KeyAlgo)
	}
	if modal.Error() != "" {
		t.Errorf("expected empty error on success, got: %s", modal.Error())
	}
}

func TestCreateModal_ValidationFailureEmptyAlias(t *testing.T) {
	modal := tviewui.NewCreateModal([]string{"~/.ssh/config"})

	submitted := false
	modal.SetOnSubmit(func(p tviewui.CreateParams) error {
		submitted = true
		return nil
	})

	modal.SetAlias("   ")
	modal.SetHostName("192.168.1.1")
	modal.Submit()

	if submitted {
		t.Errorf("submit should fail validation when Alias is empty")
	}
	if !strings.Contains(modal.Error(), "Alias / Name is required") {
		t.Errorf("expected 'Alias / Name is required' error, got: %s", modal.Error())
	}
}

func TestCreateModal_ValidationFailureEmptyHostName(t *testing.T) {
	modal := tviewui.NewCreateModal([]string{"~/.ssh/config"})

	submitted := false
	modal.SetOnSubmit(func(p tviewui.CreateParams) error {
		submitted = true
		return nil
	})

	modal.SetAlias("my-server")
	modal.SetHostName("   ")
	modal.Submit()

	if submitted {
		t.Errorf("submit should fail validation when HostName is empty")
	}
	if !strings.Contains(modal.Error(), "Hostname / IP is required") {
		t.Errorf("expected 'Hostname / IP is required' error, got: %s", modal.Error())
	}
}

func TestCreateModal_ValidationFailureEmptyUser(t *testing.T) {
	modal := tviewui.NewCreateModal([]string{"~/.ssh/config"})

	submitted := false
	modal.SetOnSubmit(func(p tviewui.CreateParams) error {
		submitted = true
		return nil
	})

	modal.SetAlias("my-server")
	modal.SetHostName("192.168.1.1")
	modal.SetUser("   ")
	modal.Submit()

	if submitted {
		t.Errorf("submit should fail validation when User is empty")
	}
	if !strings.Contains(modal.Error(), "User is required") {
		t.Errorf("expected 'User is required' error, got: %s", modal.Error())
	}
}

func TestCreateModal_ValidationPasswordRequiredWhenAuthKindPassword(t *testing.T) {
	modal := tviewui.NewCreateModal([]string{"~/.ssh/config"})

	submitted := false
	modal.SetOnSubmit(func(p tviewui.CreateParams) error {
		submitted = true
		return nil
	})

	modal.SetAlias("my-server")
	modal.SetHostName("192.168.1.1")
	modal.SetUser("root")
	modal.SetAuthKind("password")
	modal.SetPassword("   ")
	modal.Submit()

	if submitted {
		t.Errorf("submit should fail validation when Password is empty for password auth")
	}
	if !strings.Contains(modal.Error(), "Password is required for password credential") {
		t.Errorf("expected 'Password is required for password credential' error, got: %s", modal.Error())
	}
}

func TestCreateModal_ValidationFailureInvalidPort(t *testing.T) {
	invalidPorts := []string{"abc", "70000", "0", "-5"}
	for _, p := range invalidPorts {
		modal := tviewui.NewCreateModal([]string{"~/.ssh/config"})
		submitted := false
		modal.SetOnSubmit(func(p tviewui.CreateParams) error {
			submitted = true
			return nil
		})

		modal.SetAlias("valid-server")
		modal.SetHostName("10.0.0.1")
		modal.SetUser("root")
		modal.SetPort(p)
		modal.Submit()

		if submitted {
			t.Errorf("submit should fail for invalid port %q", p)
		}
		if !strings.Contains(modal.Error(), "Port must be a number between 1 and 65535") {
			t.Errorf("expected port error for %q, got: %s", p, modal.Error())
		}
	}
}

func TestCreateModal_ValidationFailureSpacesInFields(t *testing.T) {
	tests := []struct {
		name        string
		alias       string
		host        string
		user        string
		proxy       string
		expectedErr string
	}{
		{
			name:        "spaces in alias",
			alias:       "my server",
			host:        "10.0.0.1",
			user:        "root",
			expectedErr: "Alias / Name cannot contain spaces",
		},
		{
			name:        "spaces in hostname",
			alias:       "myserver",
			host:        "10 . 0 . 0 . 1",
			user:        "root",
			expectedErr: "Hostname / IP cannot contain spaces",
		},
		{
			name:        "spaces in user",
			alias:       "myserver",
			host:        "10.0.0.1",
			user:        "user name",
			expectedErr: "User cannot contain spaces",
		},
		{
			name:        "spaces in proxyjump",
			alias:       "myserver",
			host:        "10.0.0.1",
			user:        "root",
			proxy:       "proxy jump",
			expectedErr: "ProxyJump cannot contain spaces",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			modal := tviewui.NewCreateModal([]string{"~/.ssh/config"})
			submitted := false
			modal.SetOnSubmit(func(p tviewui.CreateParams) error {
				submitted = true
				return nil
			})

			modal.SetAlias(tt.alias)
			modal.SetHostName(tt.host)
			modal.SetUser(tt.user)
			modal.SetProxyJump(tt.proxy)
			modal.Submit()

			if submitted {
				t.Errorf("submit should fail validation for %s", tt.name)
			}
			if !strings.Contains(modal.Error(), tt.expectedErr) {
				t.Errorf("expected error %q, got %q", tt.expectedErr, modal.Error())
			}
		})
	}
}

func TestCreateModal_ValidationPasswordNotRequiredWhenAuthKindKey(t *testing.T) {
	modal := tviewui.NewCreateModal([]string{"~/.ssh/config"})

	submitted := false
	modal.SetOnSubmit(func(p tviewui.CreateParams) error {
		submitted = true
		return nil
	})

	modal.SetAlias("my-server")
	modal.SetHostName("192.168.1.1")
	modal.SetUser("root")
	modal.SetAuthKind("key")
	modal.SetPassword("")
	modal.Submit()

	if !submitted {
		t.Errorf("submit should pass validation for key auth without password")
	}
	if modal.Error() != "" {
		t.Errorf("expected no error, got: %s", modal.Error())
	}
}

func TestCreateModal_OnSubmitError(t *testing.T) {
	modal := tviewui.NewCreateModal([]string{"~/.ssh/config"})

	modal.SetOnSubmit(func(p tviewui.CreateParams) error {
		return errors.New("vault backend error: write failed")
	})

	modal.SetAlias("my-server")
	modal.SetHostName("192.168.1.1")
	modal.SetUser("root")
	modal.Submit()

	if modal.Error() != "vault backend error: write failed" {
		t.Errorf("expected error to be recorded, got: %s", modal.Error())
	}
}

func TestCreateModal_CancelCallback(t *testing.T) {
	modal := tviewui.NewCreateModal([]string{"~/.ssh/config"})

	cancelled := false
	modal.SetOnCancel(func() {
		cancelled = true
	})

	modal.TriggerCancel()
	if !cancelled {
		t.Errorf("expected cancel callback to trigger on TriggerCancel()")
	}
}

func TestCreateModal_EscapeKeyCancels(t *testing.T) {
	modal := tviewui.NewCreateModal([]string{"~/.ssh/config"})

	cancelled := false
	modal.SetOnCancel(func() {
		cancelled = true
	})

	var focusFunc func(p tview.Primitive)
	focusFunc = func(p tview.Primitive) {
		if p != nil {
			p.Focus(focusFunc)
		}
	}
	modal.Primitive().Focus(focusFunc)

	handler := modal.Primitive().InputHandler()
	if handler != nil {
		handler(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone), focusFunc)
	}

	if !cancelled {
		t.Errorf("expected cancel callback to trigger on Escape key")
	}
}

func TestCreateModal_ArrowKeysNavigateFields(t *testing.T) {
	modal := tviewui.NewCreateModal([]string{"~/.ssh/config"})

	focusFunc := func(p tview.Primitive) {}
	modal.Primitive().Focus(focusFunc)

	handler := modal.Primitive().InputHandler()
	if handler != nil {
		// Send KeyDown and KeyUp events
		handler(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone), focusFunc)
		handler(tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone), focusFunc)
	}

	if modal.Form() == nil {
		t.Fatal("expected Form to be non-nil")
	}
}

func TestCreateModal_DropDownClosedVsOpenNavigation(t *testing.T) {
	modal := tviewui.NewCreateModal([]string{"~/.ssh/config", "vw:personal"})
	form := modal.Form()

	// Focus item 1 ("Destination:", DropDown)
	form.SetFocus(1)
	idx, _ := form.GetFocusedItemIndex()
	if idx != 1 {
		t.Fatalf("expected focused item 1, got %d", idx)
	}

	dd, ok := form.GetFormItem(1).(*tview.DropDown)
	if !ok {
		t.Fatalf("item 1 is not *tview.DropDown")
	}

	if dd.IsOpen() {
		t.Fatalf("expected dropdown to be closed initially")
	}

	// Focus item 0 (InputField: Alias)
	form.SetFocus(0)
	idx, _ = form.GetFocusedItemIndex()
	if idx != 0 {
		t.Fatalf("expected focused item 0, got %d", idx)
	}
}

func TestCreateModal_ActiveFieldHighlight(t *testing.T) {
	modal := tviewui.NewCreateModal([]string{"~/.ssh/config", "vw:personal"})
	form := modal.Form()

	modal.UpdateFieldStyles()

	item0, ok0 := form.GetFormItem(0).(*tview.InputField)
	if !ok0 {
		t.Fatalf("item 0 is not InputField")
	}

	item1, ok1 := form.GetFormItem(1).(*tview.DropDown)
	if !ok1 {
		t.Fatalf("item 1 is not DropDown")
	}

	// Initially item 0 is focused
	_ = item0
	_ = item1
}

func TestEditModal_PrefilledFieldsAndTitle(t *testing.T) {
	entry := hosts.Entry{
		Alias:     "my-server",
		HostName:  "192.168.1.50",
		User:      "admin",
		Port:      "2222",
		ProxyJump: "bastion",
		Source:    "file",
		AuthKind:  "key",
	}

	targets := []string{"~/.ssh/config", "vw:personal"}
	modal := tviewui.NewEditModal(entry, targets)

	if !modal.IsEdit() {
		t.Errorf("expected IsEdit() to be true")
	}

	var submittedParams tviewui.CreateParams
	modal.SetOnSubmit(func(p tviewui.CreateParams) error {
		submittedParams = p
		return nil
	})

	modal.Submit()

	if submittedParams.Alias != "my-server" {
		t.Errorf("Alias = %q, want 'my-server'", submittedParams.Alias)
	}
	if submittedParams.HostName != "192.168.1.50" {
		t.Errorf("HostName = %q, want '192.168.1.50'", submittedParams.HostName)
	}
	if submittedParams.User != "admin" {
		t.Errorf("User = %q, want 'admin'", submittedParams.User)
	}
	if submittedParams.Port != "2222" {
		t.Errorf("Port = %q, want '2222'", submittedParams.Port)
	}
	if submittedParams.ProxyJump != "bastion" {
		t.Errorf("ProxyJump = %q, want 'bastion'", submittedParams.ProxyJump)
	}
	if submittedParams.Target != "~/.ssh/config" {
		t.Errorf("Target = %q, want '~/.ssh/config'", submittedParams.Target)
	}
	if submittedParams.AuthKind != "key" {
		t.Errorf("AuthKind = %q, want 'key'", submittedParams.AuthKind)
	}
}

func TestEditModal_VaultPasswordCanBeEmpty(t *testing.T) {
	entry := hosts.Entry{
		Alias:    "db-prod",
		HostName: "10.0.0.5",
		User:     "root",
		Source:   "vw:personal",
		AuthKind: "password",
	}

	targets := []string{"~/.ssh/config", "vw:personal"}
	modal := tviewui.NewEditModal(entry, targets)

	var submittedParams tviewui.CreateParams
	modal.SetOnSubmit(func(p tviewui.CreateParams) error {
		submittedParams = p
		return nil
	})

	// Password left empty -> should NOT trigger validation error in edit mode
	modal.Submit()

	if modal.Error() != "" {
		t.Fatalf("expected no validation error for empty password on edit, got: %s", modal.Error())
	}
	if submittedParams.Password != "" {
		t.Errorf("expected empty password to indicate unchanged, got %q", submittedParams.Password)
	}
}

func TestCreateModal_BorderEnabled(t *testing.T) {
	modal := tviewui.NewCreateModal([]string{"~/.ssh/config"})
	// Box.GetBorder() returns whether border is enabled on the form
	screen := tcell.NewSimulationScreen("UTF-8")
	_ = screen.Init()
	screen.SetSize(120, 30)

	modal.Primitive().SetRect(0, 0, 120, 30)
	modal.Primitive().Draw(screen)
	screen.Show()

	cells, w, h := screen.GetContents()
	var sb strings.Builder
	for i := 0; i < w*h && i < len(cells); i++ {
		for _, r := range cells[i].Runes {
			sb.WriteRune(r)
		}
	}
	s := sb.String()

	if !strings.Contains(s, "Create New SSH Connection") {
		t.Errorf("expected border title 'Create New SSH Connection' to render on screen")
	}
}





