package tviewui_test

import (
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/ac-kurniawan/wardenssh/internal/config"
	"github.com/ac-kurniawan/wardenssh/internal/hosts"
	"github.com/ac-kurniawan/wardenssh/internal/tviewui"
)

// TestAddVaultModalEnterAdvancesToPasswordOnVisibleForm covers the user path
// shown in the add-vault form: Enter from Email must focus Password, not submit
// prematurely, and the Password/Add/Cancel rows must all fit in the modal.
func TestAddVaultModalEnterAdvancesToPasswordOnVisibleForm(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatalf("screen init: %v", err)
	}
	screen.SetSize(100, 24)
	app := tview.NewApplication().SetScreen(screen)

	m := tviewui.NewAddVaultModal(nil, config.CustomFields{}, hosts.NewList(nil))
	m.SetName("work")
	m.SetApplication(app)
	app.SetRoot(m.Primitive(), true)
	runDone := make(chan error, 1)
	go func() { runDone <- app.Run() }()
	t.Cleanup(func() {
		app.Stop()
		<-runDone
	})

	for range 3 {
		screen.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
		time.Sleep(20 * time.Millisecond)
	}
	screen.InjectKey(tcell.KeyRune, 'p', tcell.ModNone)
	time.Sleep(20 * time.Millisecond)

	if got := m.Password(); got != "p" {
		t.Fatalf("typing after Enter from Email changed Password to %q, want %q (Enter should advance focus)", got, "p")
	}
	if got := m.Error(); got != "" {
		t.Fatalf("Enter submitted before password was entered: %q", got)
	}
	if !screenHas(screen, "Password:") || !screenHas(screen, "Add") || !screenHas(screen, "Cancel") {
		t.Fatal("modal clipped Password/Add/Cancel; all form controls must be visible")
	}
}
