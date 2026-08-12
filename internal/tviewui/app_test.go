package tviewui_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ac-kurniawan/wardenssh/internal/config"
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
	if !quit {
		t.Error("expected RequestQuit to return true (immediate quit) with no live sessions")
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





