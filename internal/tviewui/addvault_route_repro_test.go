package tviewui_test

import (
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
)

// TestReproCtrlAThroughRealScreen: Ctrl+A must open the add-vault modal when
// the event travels the full path (terminal -> app capture -> handler), not
// just when handleGlobalKeys is called directly.
func TestReproCtrlAThroughRealScreen(t *testing.T) {
	app, screen := startRealApp(t)
	t.Cleanup(func() { app.StopForTest() })

	waitForPane(t, app, "host")

	screen.InjectKey(tcell.KeyCtrlA, 0, tcell.ModNone)

	deadline := time.Now().Add(2 * time.Second)
	for !app.InAddVaultModal() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !app.InAddVaultModal() {
		t.Fatal("Ctrl+A through the real screen did not open the add-vault modal")
	}
}
