package tviewui_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/ac-kurniawan/wardenssh/internal/config"
	"github.com/ac-kurniawan/wardenssh/internal/hosts"
	"github.com/ac-kurniawan/wardenssh/internal/tviewui"
	"github.com/ac-kurniawan/wardenssh/internal/vault"
	"github.com/ac-kurniawan/wardenssh/internal/vaultadapter"
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
func (f *failingVaultClient) Sync() error            { return fmt.Errorf("network connection failed") }

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





