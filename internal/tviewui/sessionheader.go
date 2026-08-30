package tviewui

import (
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/mattn/go-runewidth"
	"github.com/rivo/tview"
)

const (
	pingInterval    = 3 * time.Second
	pingDialTimeout = 1500 * time.Millisecond

	pingColorHealthy  = "22C55E"
	pingColorDegraded = "F59E0B"
	pingColorFailed   = "EF4444"
	pingColorProbing  = "64748B"
)

// SessionHeader is the bar directly below the tab strip and above the
// terminal viewport — mirrors index-tui.html's session header:
// "● shopee-2 (20.198.88.91) [ACTIVE SESSION] [ 42 ms]  [Copy][Disconnect][Force Kill]"
type SessionHeader struct {
	view *tview.TextView

	mu sync.Mutex

	// session identity (for rendering and target change detection)
	alias    string
	rawHost  string
	port     string
	source   string
	focused  bool
	hasSession bool

	// ping state (mutex-guarded)
	pingSlot   string
	pingColor  string
	probing    bool
	pingTarget string // net.JoinHostPort(host,port)

	// lifecycle
	ticker *time.Ticker
	stopCh chan struct{}
	dialFn func(host, port string) (int, bool)

	// app for QueueUpdateDraw (optional, for real UI)
	app *tview.Application
}

// NewSessionHeader builds the header bar (h=1, no border).
func NewSessionHeader() *SessionHeader {
	v := tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignLeft)
	v.SetBorder(false)
	h := &SessionHeader{
		view:      v,
		pingSlot:  "[ ·· ms]",
		pingColor: pingColorProbing,
		probing:   true,
	}
	h.Clear()
	return h
}

// Primitive returns the tview primitive for layout embedding (height 1).
func (h *SessionHeader) Primitive() tview.Primitive { return h.view }

// Text returns the rendered text (tests).
func (h *SessionHeader) Text() string { return h.view.GetText(true) }

// RawText returns text with tags intact (tests).
func (h *SessionHeader) RawText() string { return h.view.GetText(false) }

// SetApp injects the tview application for QueueUpdateDraw scheduling (optional).
func (h *SessionHeader) SetApp(app *tview.Application) {
	h.mu.Lock()
	h.app = app
	h.mu.Unlock()
}

// Clear shows the idle state when no session exists.
func (h *SessionHeader) Clear() {
	h.mu.Lock()
	h.hasSession = false
	h.alias = ""
	h.rawHost = ""
	h.port = ""
	h.source = ""
	h.focused = false
	if h.stopCh != nil {
		close(h.stopCh)
		h.stopCh = nil
	}
	if h.ticker != nil {
		h.ticker.Stop()
		h.ticker = nil
	}
	h.pingSlot = "[ ·· ms]"
	h.pingColor = pingColorProbing
	h.probing = true
	h.pingTarget = ""
	h.mu.Unlock()
	h.view.SetText("[#64748B]○ No active session — select a host from the left[-]")
}

// SetSession updates the header for the active session.
// alias/host/source from terminalSession, up is elapsed, focused controls badge.
// Up is kept for signature compatibility (unused for ping display).
func (h *SessionHeader) SetSession(alias, host, source string, up time.Duration, focused bool) {
	h.SetSessionWithPort(alias, host, "22", source, up, focused)
}

// SetSessionWithPort updates the header with explicit host+port for ping probing.
// Port defaults to "22" when empty.
func (h *SessionHeader) SetSessionWithPort(alias, host, port, source string, up time.Duration, focused bool) {
	_ = up
	if alias == "" {
		h.Clear()
		return
	}
	if port == "" {
		port = "22"
	}
	newTarget := net.JoinHostPort(host, port)

	h.mu.Lock()
	changed := !h.hasSession || newTarget != h.pingTarget || alias != h.alias || source != h.source
	// store session fields
	h.alias = alias
	h.rawHost = host
	h.port = port
	h.source = source
	h.focused = focused
	h.hasSession = true

	var needStart bool
	var hostCopy, portCopy string
	var stopCh chan struct{}
	var ticker *time.Ticker

	if changed {
		// stop previous ticker/goroutine
		if h.stopCh != nil {
			close(h.stopCh)
			h.stopCh = nil
		}
		if h.ticker != nil {
			h.ticker.Stop()
			h.ticker = nil
		}
		h.pingSlot = "[ ·· ms]"
		h.pingColor = pingColorProbing
		h.probing = true
		h.pingTarget = newTarget

		stopCh = make(chan struct{})
		ticker = time.NewTicker(pingInterval)
		h.stopCh = stopCh
		h.ticker = ticker
		hostCopy = host
		portCopy = port
		needStart = true
	}
	slot := h.pingSlot
	color := h.pingColor
	h.mu.Unlock()

	if needStart {
		// render probing immediately
		h.renderInternal(alias, host, source, focused, "[ ·· ms]", pingColorProbing)
		go h.pingLoop(hostCopy, portCopy, stopCh, ticker)
		return
	}
	h.renderInternal(alias, host, source, focused, slot, color)
}

func (h *SessionHeader) renderInternal(alias, host, source string, focused bool, pingSlot, pingColor string) {
	addr := host
	if runewidth.StringWidth(addr) > 24 {
		addr = runewidth.Truncate(addr, 23, "…")
	}
	state := "CONNECTED"
	if focused {
		state = "ACTIVE SESSION"
	}
	dot := "[#64748B]○[-]"
	if source != "file" || true { // live dot always green when session exists
		dot = "[#22C55E]●[-]"
	}
	sourcePill := ""
	if source != "" && source != "file" {
		short := source
		if len(short) > 3 && short[:3] == "vw:" {
			short = short[3:]
		}
		sourcePill = fmt.Sprintf(" [#1E293B]%s[-]", tview.Escape(short))
	} else if source == "file" {
		sourcePill = " [#334155]local[-]"
	}
	agent := ""
	if source != "file" && source != "" {
		agent = " [#64748B]•[-] [#0F1E14:#22C55E] Agent: RAM-only [-]"
	}
	ping := fmt.Sprintf(" [#%s]%s[-]", pingColor, pingSlot)
	left := fmt.Sprintf("%s [#F8FAFC]%s[-] [#64748B](%s)[-]%s [#0F1E14:#22C55E] %s [-]%s%s",
		dot, tview.Escape(alias), tview.Escape(addr), sourcePill, state, ping, agent)

	right := " [#334155]⎘ Copy[-] [#334155]⏻ Disconnect[-] [#7C4D00]Force Kill[-]"
	text := left + right
	h.view.SetText(text)
}

// pingLoop runs the periodic probe for the active target.
func (h *SessionHeader) pingLoop(host, port string, stopCh chan struct{}, ticker *time.Ticker) {
	h.doProbe(host, port, stopCh)
	for {
		select {
		case <-ticker.C:
			h.doProbe(host, port, stopCh)
		case <-stopCh:
			return
		}
	}
}

func (h *SessionHeader) doProbe(host, port string, stopCh chan struct{}) {
	ms, ok := h.dial(host, port)
	// check staleness: if stopCh is no longer current, discard
	h.mu.Lock()
	if h.stopCh != stopCh || h.pingTarget != net.JoinHostPort(host, port) {
		h.mu.Unlock()
		return
	}
	h.mu.Unlock()
	h.updatePingResult(ms, ok, stopCh)
}

func (h *SessionHeader) dial(host, port string) (int, bool) {
	fn := h.getDialFn()
	if fn != nil {
		return fn(host, port)
	}
	return h.defaultDial(host, port)
}

func (h *SessionHeader) getDialFn() func(host, port string) (int, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.dialFn
}

func (h *SessionHeader) defaultDial(host, port string) (int, bool) {
	start := time.Now()
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), pingDialTimeout)
	elapsed := int(time.Since(start).Milliseconds())
	if err != nil {
		return 0, false
	}
	_ = conn.Close()
	if elapsed > 999 {
		elapsed = 999
	}
	return elapsed, true
}

func (h *SessionHeader) updatePingResult(ms int, ok bool, stopCh chan struct{}) {
	h.mu.Lock()
	// if this result is stale (target changed or stopped), ignore
	if h.stopCh != stopCh {
		h.mu.Unlock()
		return
	}
	var slot, color string
	if !ok {
		slot = "[--- ms]"
		color = pingColorFailed
	} else {
		if ms < 0 {
			ms = 0
		}
		if ms > 999 {
			ms = 999
		}
		if ms < 120 {
			slot = fmt.Sprintf("[%3d ms]", ms)
			color = pingColorHealthy
		} else {
			slot = fmt.Sprintf("[%3d ms]", ms)
			color = pingColorDegraded
		}
	}
	h.pingSlot = slot
	h.pingColor = color
	h.probing = false
	alias := h.alias
	host := h.rawHost
	source := h.source
	focused := h.focused
	has := h.hasSession
	app := h.app
	h.mu.Unlock()

	if !has {
		return
	}
	if app != nil {
		app.QueueUpdateDraw(func() {
			h.renderInternal(alias, host, source, focused, slot, color)
		})
	} else {
		h.renderInternal(alias, host, source, focused, slot, color)
	}
}

// SetDialFuncForTest injects a mock dial function (tests).
func (h *SessionHeader) SetDialFuncForTest(fn func(host, port string) (int, bool)) {
	h.mu.Lock()
	h.dialFn = fn
	h.mu.Unlock()
}

// SetPingResultForTest injects a ping result directly (tests).
func (h *SessionHeader) SetPingResultForTest(ms int, ok bool) {
	h.mu.Lock()
	stopCh := h.stopCh
	h.mu.Unlock()
	h.updatePingResult(ms, ok, stopCh)
}

// PingTargetForTest returns the current dial target (tests).
func (h *SessionHeader) PingTargetForTest() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.pingTarget
}

// PingSlotForTest returns current ping slot (tests).
func (h *SessionHeader) PingSlotForTest() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.pingSlot
}

// PingColorForTest returns current ping color (tests).
func (h *SessionHeader) PingColorForTest() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.pingColor
}
