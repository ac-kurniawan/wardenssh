package tviewui_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/ac-kurniawan/wardenssh/internal/config"
	"github.com/ac-kurniawan/wardenssh/internal/hosts"
	"github.com/ac-kurniawan/wardenssh/internal/tviewui"
	"github.com/ac-kurniawan/wardenssh/internal/vault"
	"github.com/ac-kurniawan/wardenssh/internal/vaultclient"
)

func sampleVaults() []config.Vault {
	return []config.Vault{
		{Name: "vw", Server: "https://vw.example.com", Email: "user@example.com"},
	}
}

func TestSetupModalInitialState(t *testing.T) {
	hl := hosts.NewList(nil)
	m := tviewui.NewSetupModal(sampleVaults(), config.CustomFields{}, hl)

	if m.IsDone() {
		t.Fatal("expected setup to not be done initially")
	}
	prompt := m.CurrentPrompt()
	if !strings.Contains(prompt, "vw") {
		t.Errorf("prompt should contain 'vw', got: %s", prompt)
	}
	if !strings.Contains(prompt, "user@example.com") {
		t.Errorf("prompt should contain email, got: %s", prompt)
	}
}

// TestSetupModalShowsServerAndUsername: the vault unlock modal must show the
// vault server URL and username (email) in the form body — not just in the
// title — so the user can confirm WHICH vault they are unlocking before typing
// a master password. The fields are read-only (display-only).
func TestSetupModalShowsServerAndUsername(t *testing.T) {
	hl := hosts.NewList(nil)
	m := tviewui.NewSetupModal(sampleVaults(), config.CustomFields{}, hl)

	if got := m.ServerField(); got != "https://vw.example.com" {
		t.Errorf("ServerField() = %q, want https://vw.example.com", got)
	}
	if got := m.UsernameField(); got != "user@example.com" {
		t.Errorf("UsernameField() = %q, want user@example.com", got)
	}
}

// TestSetupModalServerUsernameUpdateOnVaultAdvance: when skipping/advancing to
// the next vault, the server and username fields must update to the new vault.
func TestSetupModalServerUsernameUpdateOnVaultAdvance(t *testing.T) {
	vaults := []config.Vault{
		{Name: "vw1", Server: "https://vw1.example.com", Email: "u1@e.com"},
		{Name: "vw2", Server: "https://vw2.example.com", Email: "u2@e.com"},
	}
	hl := hosts.NewList(nil)
	m := tviewui.NewSetupModal(vaults, config.CustomFields{}, hl)

	if got := m.ServerField(); got != "https://vw1.example.com" {
		t.Fatalf("initial ServerField() = %q, want https://vw1.example.com", got)
	}
	if got := m.UsernameField(); got != "u1@e.com" {
		t.Fatalf("initial UsernameField() = %q, want u1@e.com", got)
	}

	m.SkipCurrent()

	if got := m.ServerField(); got != "https://vw2.example.com" {
		t.Errorf("after skip ServerField() = %q, want https://vw2.example.com", got)
	}
	if got := m.UsernameField(); got != "u2@e.com" {
		t.Errorf("after skip UsernameField() = %q, want u2@e.com", got)
	}
}

func TestSetupModalTypingBuildsPassword(t *testing.T) {
	hl := hosts.NewList(nil)
	m := tviewui.NewSetupModal(sampleVaults(), config.CustomFields{}, hl)
	m.TypeRune('p')
	m.TypeRune('a')
	m.TypeRune('s')
	m.TypeRune('s')
	if got := m.Password(); got != "pass" {
		t.Errorf("password = %q, want pass", got)
	}
}

func TestSetupModalSkipAllVaults(t *testing.T) {
	hl := hosts.NewList(nil)
	m := tviewui.NewSetupModal(sampleVaults(), config.CustomFields{}, hl)

	skipped := false
	m.SetOnSkip(func() {
		skipped = true
	})
	m.SkipCurrent()
	if !skipped {
		t.Fatal("expected OnSkip callback after skipping all vaults")
	}
	if !m.IsDone() {
		t.Fatal("expected setup to be done after skipping all vaults")
	}
}

func TestSetupModalMultiVaultSkipAdvances(t *testing.T) {
	vaults := []config.Vault{
		{Name: "vw1", Server: "https://vw1.example.com", Email: "u1@e.com"},
		{Name: "vw2", Server: "https://vw2.example.com", Email: "u2@e.com"},
	}
	hl := hosts.NewList(nil)
	m := tviewui.NewSetupModal(vaults, config.CustomFields{}, hl)

	m.SkipCurrent()
	if m.IsDone() {
		t.Fatal("expected to still be in setup for vw2 after skipping vw1")
	}
	if !strings.Contains(m.CurrentPrompt(), "vw2") {
		t.Errorf("expected prompt for vw2, got: %s", m.CurrentPrompt())
	}

	m.SkipCurrent()
	if !m.IsDone() {
		t.Fatal("expected to be done after skipping all vaults")
	}
}

func TestSetupModalBackspaceDeletesLastChar(t *testing.T) {
	hl := hosts.NewList(nil)
	m := tviewui.NewSetupModal(sampleVaults(), config.CustomFields{}, hl)
	m.TypeRune('h')
	m.TypeRune('i')
	m.Backspace()
	if got := m.Password(); got != "h" {
		t.Errorf("password after backspace = %q, want h", got)
	}
}

func TestSetupModalSavesRefreshTokenToKeyringOnLogin(t *testing.T) {
	var savedVaultName, savedRefreshToken string
	tviewui.SetKeyringSetRefreshTokenForTest(func(vName, token string) error {
		savedVaultName = vName
		savedRefreshToken = token
		return nil
	})
	defer tviewui.ResetKeyringSetRefreshTokenForTest()

	ak, err := vaultclient.DeriveAccountKeys("user@example.com", "pass", 1000)
	if err != nil {
		t.Fatalf("DeriveAccountKeys: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/identity/accounts/prelogin", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"kdf":0,"kdfIterations":1000}`))
	})
	mux.HandleFunc("/identity/connect/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]string{
			"access_token":  "mock-at",
			"refresh_token": "mock-rt-456",
			"Key":           ak.ProtectedKey,
		}
		json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/ciphers", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":[]}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	vaults := []config.Vault{
		{Name: "myvault", Server: srv.URL, Email: "user@example.com"},
	}
	hl := hosts.NewList(nil)
	m := tviewui.NewSetupModal(vaults, config.CustomFields{}, hl)

	doneCh := make(chan struct{})
	m.SetOnComplete(func(vc vault.Client) {
		close(doneCh)
	})

	m.TypeRune('p')
	m.TypeRune('a')
	m.TypeRune('s')
	m.TypeRune('s')
	m.Submit()

	select {
	case <-doneCh:
	case <-time.After(3 * time.Second):
		t.Fatalf("setup did not complete in time, last err: %s", m.Error())
	}

	if savedVaultName != "myvault" {
		t.Errorf("savedVaultName = %q, want myvault", savedVaultName)
	}
	if savedRefreshToken != "mock-rt-456" {
		t.Errorf("savedRefreshToken = %q, want mock-rt-456", savedRefreshToken)
	}
}

// Compile-time check that vault.Client is used in the callback signature.
var _ vault.Client = (vault.Client)(nil)

// TestSetupModalFailedLoginShowsErrorOnScreen: a failed login (wrong master
// password) must visibly repaint the modal so the user SEES the failure.
// The login runs on a background goroutine; tview only repaints after input
// events or QueueUpdateDraw, so if the error-path title update is applied
// off-loop without scheduling a draw, the screen keeps showing the pristine
// title and the user gets no feedback at all. This drives a real headless
// application and reads the simulation screen, not just modal state.
func TestSetupModalFailedLoginShowsErrorOnScreen(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/identity/accounts/prelogin", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatalf("screen init: %v", err)
	}
	screen.SetSize(120, 30)

	app := tview.NewApplication().SetScreen(screen)
	hl := hosts.NewList(nil)
	vaults := []config.Vault{
		{Name: "myvault", Server: srv.URL, Email: "user@example.com"},
	}
	m := tviewui.NewSetupModal(vaults, config.CustomFields{}, hl, true)
	m.SetApplication(app)
	app.SetRoot(m.Primitive(), true)

	runDone := make(chan error, 1)
	go func() { runDone <- app.Run() }()
	defer func() {
		app.Stop()
		<-runDone
	}()

	m.TypeRune('w')
	m.TypeRune('r')
	m.TypeRune('o')
	m.TypeRune('n')
	m.TypeRune('g')
	m.Submit()

	deadline := time.Now().Add(3 * time.Second)
	for {
		cells, width, height := screen.GetContents()
		var sb strings.Builder
		for i := 0; i < width*height && i < len(cells); i++ {
			for _, r := range cells[i].Runes {
				sb.WriteRune(r)
			}
		}
		if strings.Contains(sb.String(), "login") {
			return // error message became visible
		}
		if time.Now().After(deadline) {
			t.Fatalf("login failure message never appeared on screen; last errMsg=%q", m.Error())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestSetupModalEnterKeySubmits(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/identity/accounts/prelogin", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	vaults := []config.Vault{
		{Name: "myvault", Server: srv.URL, Email: "user@example.com"},
	}
	hl := hosts.NewList(nil)
	m := tviewui.NewSetupModal(vaults, config.CustomFields{}, hl, true)
	m.TypeRune('p')
	m.TypeRune('a')
	m.TypeRune('s')
	m.TypeRune('s')

	var focusFunc func(p tview.Primitive)
	focusFunc = func(p tview.Primitive) {
		if p != nil {
			p.Focus(focusFunc)
		}
	}
	m.Primitive().Focus(focusFunc)

	handler := m.Primitive().InputHandler()
	if handler != nil {
		handler(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), focusFunc)
	}

	time.Sleep(100 * time.Millisecond)
	if m.Error() == "" {
		t.Error("expected Submit() to be called on Enter press, got empty error")
	}
}


