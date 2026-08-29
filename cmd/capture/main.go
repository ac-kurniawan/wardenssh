package main

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"time"
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
	return fmt.Sprintf("#%06X", h & 0xFFFFFF)
}

func dumpScreen(screen tcell.SimulationScreen, path string) error {
	cells, w, h := screen.GetContents()
	dump := Dump{Width: w, Height: h}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			cell := cells[y*w+x]
			var ch string
			if len(cell.Runes) > 0 {
				ch = string(cell.Runes[0])
			} else {
				ch = " "
			}
			fg, bg, attr := cell.Style.Decompose()
			fgHex := colorToHex(fg)
			bgHex := colorToHex(bg)
			bold := attr&tcell.AttrBold != 0
			dump.Cells = append(dump.Cells, Cell{X: x, Y: y, Rune: ch, Fg: fgHex, Bg: bgHex, Bold: bold})
		}
	}
	data, err := json.MarshalIndent(dump, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func getRoot(app *tviewui.App) tview.Primitive {
	v := reflect.ValueOf(app).Elem()
	f := v.FieldByName("root")
	if !f.IsValid() {
		panic("root field not found")
	}
	if f.IsNil() {
		panic("root is nil")
	}
	prim := reflect.NewAt(f.Type(), unsafe.Pointer(f.UnsafeAddr())).Elem().Interface().(tview.Primitive)
	return prim
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
		{Alias: "prod-db-01", HostName: "10.0.0.5", Source: "file", User: "ac-kurniawan"},
		{Alias: "web-02", HostName: "web.internal", Source: "file", User: "ac-kurniawan"},
		{Alias: "ci-box", HostName: "10.1.0.10", Source: "vw:personal", AuthKind: "password", User: "ac-kurniawan"},
		{Alias: "onidel-vps", HostName: "onidel.tailbf5fbd.ts.net", Source: "vw:work", User: "ac-kurniawan"},
		{Alias: "shopee-2", HostName: "139.59.120.44", Source: "vw:work", User: "ac-kurniawan"},
		{Alias: "homeserver", HostName: "192.168.1.10", Source: "file", User: "ac-kurniawan"},
	})
}

func setupAppWithScreen(hl *hosts.List, vaults []config.Vault) (*tviewui.App, tcell.SimulationScreen) {
	app := tviewui.New(hl, tviewui.Deps{}, vaults)
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		panic(err)
	}
	app.SetScreenForTest(screen)
	screen.SetSize(120, 32)
	root := getRoot(app)
	root.SetRect(0, 0, 120, 32)
	return app, screen
}

func drawAndDump(app *tviewui.App, screen tcell.SimulationScreen, path string) {
	root := getRoot(app)
	screen.SetSize(120, 32)
	root.SetRect(0, 0, 120, 32)
	screen.Clear()
	root.Draw(screen)
	screen.Show()
	if err := dumpScreen(screen, path); err != nil {
		panic(err)
	}
	w, h := screen.Size()
	fmt.Printf("dumped %s %dx%d\n", path, w, h)
}

func main() {
	vaults := []config.Vault{{Name: "personal"}, {Name: "work"}}

	hl1 := sampleHosts()
	app1, screen1 := setupAppWithScreen(hl1, vaults)
	app1.HostPane().SetSyncStatus("Synced 15:04")
	app1.HostPane().Refresh()
	app1.TopBar().SetSyncStatus("Synced 15:04")
	app1.HostPane().SetFocused(true)
	drawAndDump(app1, screen1, "/tmp/capture-01-idle.json")
	screen1.Fini()

	hl2 := sampleHosts()
	app2, screen2 := setupAppWithScreen(hl2, vaults)
	app2.HostPane().SetSyncStatus("Synced 15:04")
	app2.TopBar().SetSyncStatus("Synced 15:04")
	app2.HostPane().SetFilter("shopee")
	app2.HostPane().Refresh()
	app2.HostPane().SetFocused(true)
	drawAndDump(app2, screen2, "/tmp/capture-02-filter.json")
	screen2.Fini()

	hl3 := sampleHosts()
	hl3.MarkLive("prod-db-01", "file")
	app3, screen3 := setupAppWithScreen(hl3, vaults)
	app3.HostPane().SetSyncStatus("Synced 15:04")
	app3.TopBar().SetSyncStatus("Synced 15:04")
	key3 := tviewui.SessionKey("prod-db-01", "file")
	app3.TerminalPane().SetSessionForTest(key3, "prod-db-01", "file")
	tv := tview.NewTextView().SetText("root@prod-db-01:~# ls\nfile1.txt\nfile2.log\nroot@prod-db-01:~# ").SetBorder(true)
	tv.SetTitle(fmt.Sprintf(" 💻 prod-db-01 (10.0.0.5) [ACTIVE SESSION] · Up: 12s "))
	app3.TerminalPane().SetSessionViewForTest(key3, tv)
	app3.HostPane().SetActiveKey(key3)
	app3.HostPane().Refresh()
	app3.TopBar().SetSessionCounts(1, 1)
	keys := []string{key3}
	aliases := map[string]string{key3: "prod-db-01"}
	hostsMap := map[string]string{key3: "10.0.0.5"}
	app3.TabBar().Update(keys, key3, aliases, hostsMap)
	app3.ShowTerminalPaneForTest()
	app3.FocusTerminal()
	drawAndDump(app3, screen3, "/tmp/capture-03-one-active.json")
	screen3.Fini()

	hl4 := sampleHosts()
	hl4.MarkLive("prod-db-01", "file")
	hl4.MarkLive("shopee-2", "vw:work")
	app4, screen4 := setupAppWithScreen(hl4, vaults)
	app4.HostPane().SetSyncStatus("Synced 15:04")
	app4.TopBar().SetSyncStatus("Synced 15:04")
	key4a := tviewui.SessionKey("prod-db-01", "file")
	key4b := tviewui.SessionKey("shopee-2", "vw:work")
	app4.TerminalPane().SetSessionForTest(key4a, "prod-db-01", "file")
	tv4a := tview.NewTextView().SetText("background session").SetBorder(true)
	tv4a.SetTitle(" 💻 prod-db-01 (10.0.0.5) [CONNECTED] · Up: 45s ")
	app4.TerminalPane().SetSessionViewForTest(key4a, tv4a)
	app4.TerminalPane().SetSessionForTest(key4b, "shopee-2", "vw:work")
	tv4b := tview.NewTextView().SetText("root@shopee-2:~# status\nConnected • RAM-only\n").SetBorder(true)
	tv4b.SetTitle(" 💻 shopee-2 (139.59.120.44) [ACTIVE SESSION] · Up: 8s • Agent: RAM-only ")
	app4.TerminalPane().SetSessionViewForTest(key4b, tv4b)
	app4.TerminalPane().Activate(key4b)
	app4.HostPane().SetActiveKey(key4b)
	app4.HostPane().Refresh()
	app4.TopBar().SetSessionCounts(1, 2)
	keys4 := []string{key4a, key4b}
	aliases4 := map[string]string{key4a: "prod-db-01", key4b: "shopee-2"}
	hosts4 := map[string]string{key4a: "10.0.0.5", key4b: "139.59.120.44"}
	app4.TabBar().Update(keys4, key4b, aliases4, hosts4)
	app4.ShowTerminalPaneForTest()
	app4.FocusTerminal()
	drawAndDump(app4, screen4, "/tmp/capture-04-two-sessions-bg.json")
	screen4.Fini()

	hl5 := sampleHosts()
	hl5.MarkLive("prod-db-01", "file")
	app5, screen5 := setupAppWithScreen(hl5, vaults)
	app5.HostPane().SetSyncStatus("Synced 15:04")
	app5.TopBar().SetSyncStatus("Synced 15:04")
	app5.RequestQuit()
	drawAndDump(app5, screen5, "/tmp/capture-05-quit-modal.json")
	screen5.Fini()

	hl6 := sampleHosts()
	app6, screen6 := setupAppWithScreen(hl6, vaults)
	app6.HostPane().SetSyncStatus("Synced 15:04")
	app6.TopBar().SetSyncStatus("Synced 15:04")
	app6.ShowScopeModalForTest()
	drawAndDump(app6, screen6, "/tmp/capture-06-scope-modal.json")
	screen6.Fini()

	hl7 := sampleHosts()
	app7, screen7 := setupAppWithScreen(hl7, vaults)
	app7.HostPane().SetSyncStatus("Synced 15:04")
	app7.TopBar().SetSyncStatus("Synced 15:04")
	app7.ShowTerminalPaneForTest()
	app7.FocusTerminal()
	drawAndDump(app7, screen7, "/tmp/capture-07-empty-terminal.json")
	_ = time.Now()
	fmt.Println("all dumps done")
}
