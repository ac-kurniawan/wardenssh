package main

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"unsafe"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/ac-kurniawan/wardenssh/internal/config"
	"github.com/ac-kurniawan/wardenssh/internal/hosts"
	"github.com/ac-kurniawan/wardenssh/internal/tviewui"
)

type Cell struct {
	X    int    `json:"x"`
	Y    int    `json:"y"`
	Rune string `json:"rune"`
	Fg   string `json:"fg"`
	Bg   string `json:"bg"`
	Bold bool   `json:"bold"`
}

type Dump struct {
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Cells  []Cell `json:"cells"`
}

func colorToHex(c tcell.Color) string {
	if c == tcell.ColorDefault {
		return "default"
	}
	if !c.Valid() {
		return "default"
	}
	h := c.Hex()
	if h == 0 && c != tcell.ColorBlack {
		r, g, b := c.RGB()
		return fmt.Sprintf("#%02X%02X%02X", r, g, b)
	}
	return fmt.Sprintf("#%06X", h&0xFFFFFF)
}

func dumpScreen(screen tcell.SimulationScreen, path string) error {
	cells, w, h := screen.GetContents()
	dump := Dump{Width: w, Height: h}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			cell := cells[y*w+x]
			ch := " "
			if len(cell.Runes) > 0 {
				ch = string(cell.Runes[0])
			}
			fg, bg, attr := cell.Style.Decompose()
			dump.Cells = append(dump.Cells, Cell{
				X: x, Y: y, Rune: ch,
				Fg: colorToHex(fg), Bg: colorToHex(bg),
				Bold: attr&tcell.AttrBold != 0,
			})
		}
	}
	data, err := json.MarshalIndent(dump, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// getRoot returns the pages widget the app actually draws (modals included).
func getRoot(app *tviewui.App) tview.Primitive {
	v := reflect.ValueOf(app).Elem()
	f := v.FieldByName("overlay")
	if !f.IsValid() || f.IsNil() {
		panic("overlay field missing")
	}
	return reflect.NewAt(f.Type(), unsafe.Pointer(f.UnsafeAddr())).Elem().Interface().(tview.Primitive)
}

func getAppTviewApp(app *tviewui.App) *tview.Application {
	v := reflect.ValueOf(app).Elem()
	f := v.FieldByName("app")
	if !f.IsValid() {
		panic("app field not found")
	}
	return reflect.NewAt(f.Type(), unsafe.Pointer(f.UnsafeAddr())).Elem().Interface().(*tview.Application)
}

func sampleHosts() *hosts.List {
	return hosts.NewList([]hosts.Entry{
		{Alias: "prod-db-01", HostName: "db.example.com", Source: "file", User: "deploy"},
		{Alias: "web-02", HostName: "web.example.com", Source: "file", User: "deploy"},
		{Alias: "ci-box", HostName: "ci.example.com", Source: "vw:personal", AuthKind: "password", User: "deploy"},
		{Alias: "edge-vps", HostName: "edge.example.net", Source: "vw:work", User: "deploy"},
		{Alias: "app-02", HostName: "app.example.net", Source: "vw:work", User: "deploy"},
		{Alias: "homeserver", HostName: "home.example.lan", Source: "file", User: "deploy"},
	})
}

func setupAppWithScreen(hl *hosts.List, vaults []config.Vault) (*tviewui.App, tcell.SimulationScreen) {
	app := tviewui.New(hl, tviewui.Deps{}, vaults)
	app.SkipSetup()
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		panic(err)
	}
	app.SetScreenForTest(screen)
	screen.SetSize(160, 36)
	return app, screen
}

func drawAndDump(app *tviewui.App, screen tcell.SimulationScreen, path string) {
	root := getRoot(app)
	screen.SetSize(160, 36)
	root.SetRect(0, 0, 160, 36)
	screen.Clear()
	root.Draw(screen)
	screen.Show()
	if err := dumpScreen(screen, path); err != nil {
		panic(err)
	}
	w, h := screen.Size()
	fmt.Printf("dumped %s %dx%d\n", path, w, h)
}

func newSessionView(app *tview.Application, title string) tview.Primitive {
	return tviewui.NewTerminalViewForTest(app, " 💻 "+title+" ")
}

func synced(app *tviewui.App) {
	app.HostPane().SetSyncStatus("Synced 15:04")
	app.TopBar().SetSyncStatus("Synced 15:04")
	app.HostPane().Refresh()
}

// feedAfterLayout lays the app out and pins the view to its on-screen rect,
// then feeds the transcript. Draw resizes the emulator to that rect and keeps
// the buffer only when the size does not change.
func feedAfterLayout(app *tviewui.App, view tview.Primitive, transcript string) {
	root := getRoot(app)
	root.SetRect(0, 0, 160, 36)
	root.Draw(tcell.NewSimulationScreen("UTF-8"))
	x, y, w, h := view.GetRect()
	view.SetRect(x, y, w, h)
	tviewui.FeedTerminalForTest(view, transcript)
}

func main() {
	vaults := []config.Vault{{Name: "personal"}, {Name: "work"}}

	hl1 := sampleHosts()
	app1, screen1 := setupAppWithScreen(hl1, vaults)
	synced(app1)
	app1.HostPane().SetFocused(true)
	drawAndDump(app1, screen1, "/tmp/capture-01-idle.json")
	screen1.Fini()

	hl2 := sampleHosts()
	app2, screen2 := setupAppWithScreen(hl2, vaults)
	synced(app2)
	app2.HostPane().SetFilter("app")
	app2.HostPane().Refresh()
	app2.HostPane().SetFocused(true)
	drawAndDump(app2, screen2, "/tmp/capture-02-filter.json")
	screen2.Fini()

	hl3 := sampleHosts()
	hl3.MarkLive("prod-db-01", "file")
	app3, screen3 := setupAppWithScreen(hl3, vaults)
	synced(app3)
	key3 := tviewui.SessionKey("prod-db-01", "file")
	app3.TerminalPane().SetSessionWithHostForTest(key3, "prod-db-01", "db.example.com", "22", "file")
	view3 := newSessionView(getAppTviewApp(app3), "prod-db-01")
	app3.TerminalPane().SetSessionViewForTest(key3, view3)
	app3.ShowTerminalPaneForTest()
	app3.FocusTerminal()
	feedAfterLayout(app3, view3,
		"Last login: Thu Oct  1 15:04:12 2026 from 10.0.0.1\r\n"+
			"deploy@prod-db-01:~$ ls\r\n"+
			"bin  etc  home  var\r\n"+
			"deploy@prod-db-01:~$ ")
	drawAndDump(app3, screen3, "/tmp/capture-03-one-active.json")
	screen3.Fini()

	hl4 := sampleHosts()
	hl4.MarkLive("prod-db-01", "file")
	hl4.MarkLive("app-02", "vw:work")
	app4, screen4 := setupAppWithScreen(hl4, vaults)
	synced(app4)
	tviewApp := getAppTviewApp(app4)
	key4a := tviewui.SessionKey("prod-db-01", "file")
	key4b := tviewui.SessionKey("app-02", "vw:work")
	app4.TerminalPane().SetSessionWithHostForTest(key4a, "prod-db-01", "db.example.com", "22", "file")
	view4a := newSessionView(tviewApp, "prod-db-01")
	app4.TerminalPane().SetSessionViewForTest(key4a, view4a)
	app4.TerminalPane().SetSessionWithHostForTest(key4b, "app-02", "app.example.net", "22", "vw:work")
	view4b := newSessionView(tviewApp, "app-02")
	app4.TerminalPane().SetSessionViewForTest(key4b, view4b)
	app4.TerminalPane().Activate(key4b)
	app4.ShowTerminalPaneForTest()
	app4.FocusTerminal()
	feedAfterLayout(app4, view4b,
		"Last login: Thu Oct  1 15:03:58 2026\r\n"+
			"deploy@app-02:~$ whoami\r\n"+
			"deploy\r\n"+
			"deploy@app-02:~$ ")
	drawAndDump(app4, screen4, "/tmp/capture-04-two-sessions-bg.json")
	screen4.Fini()

	hl5 := sampleHosts()
	app5, screen5 := setupAppWithScreen(hl5, vaults)
	synced(app5)
	app5.ShowScopeModalForTest()
	drawAndDump(app5, screen5, "/tmp/capture-05-scope-modal.json")
	screen5.Fini()

	fmt.Println("all dumps done")
}
