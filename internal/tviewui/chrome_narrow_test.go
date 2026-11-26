package tviewui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/ac-kurniawan/wardenssh/internal/hosts"
)

// dumpRows renders root onto a simulation screen and returns the visible rows.
func dumpRows(t *testing.T, root tview.Primitive, width, height int) []string {
	t.Helper()
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatalf("init simulation screen: %v", err)
	}
	defer screen.Fini()
	screen.SetSize(width, height)
	root.SetRect(0, 0, width, height)
	root.Draw(screen)
	rows := make([]string, height)
	for y := range rows {
		var b strings.Builder
		for x := range width {
			ch, _, _, _ := screen.GetContent(x, y)
			if ch == 0 {
				ch = ' '
			}
			b.WriteRune(ch)
		}
		rows[y] = b.String()
	}
	return rows
}

func TestChromeRowsAtNarrowWidth(t *testing.T) {
	hl := hosts.NewList([]hosts.Entry{
		{Alias: "prod-db-01", HostName: "db.example.com", Source: "file"},
	})
	app := New(hl, Deps{}, nil)
	defer app.TerminalPane().Close()
	app.SessionHeader().SetDialFuncForTest(func(host, port string) (int, bool) { return 42, true })

	key := SessionKey("prod-db-01", "file")
	app.TerminalPane().SetSessionForTest(key, "prod-db-01", "file")
	view, _ := fedView(t)
	defer view.Close()
	app.TerminalPane().SetSessionViewForTest(key, view)
	app.TerminalPane().Activate(key)
	app.ShowTerminalPaneForTest()

	rows := dumpRows(t, app.root, 80, 24)
	joined := strings.Join(rows, "\n")
	t.Logf("80-col render:\n%s", joined)

	// The three chrome rows appear, each carrying its own field set.
	if !strings.Contains(joined, "prod-db-01") {
		t.Fatalf("no alias in the render:\n%s", joined)
	}
	// Exactly one border title row, and it holds the identity only.
	borderRows := 0
	for _, row := range rows {
		if strings.Contains(row, "💻") {
			borderRows++
			if strings.Contains(row, "Up:") || strings.Contains(row, "ms]") || strings.Contains(row, "Agent:") {
				t.Errorf("border title repeats header fields: %q", row)
			}
		}
	}
	if borderRows != 1 {
		t.Errorf("border title rows = %d, want exactly 1:\\n%s", borderRows, joined)
	}
}
