package tviewui_test

import (
	"testing"

	"github.com/ac-kurniawan/wardenssh/internal/config"
	"github.com/ac-kurniawan/wardenssh/internal/tviewui"
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

var _ = config.CustomFields{}
