package tviewui_test

import (
	"strings"
	"testing"
	"time"

	"github.com/ac-kurniawan/wardenssh/internal/tviewui"
)

func TestSessionHeaderPingSlot(t *testing.T) {
	h := tviewui.NewSessionHeader()
	h.SetDialFuncForTest(func(host, port string) (int, bool) {
		time.Sleep(100 * time.Millisecond)
		return 42, true
	})
	// idle text unchanged
	idle := h.Text()
	if !strings.Contains(idle, "No active session") {
		t.Fatalf("idle Text = %q, want No active session", idle)
	}
	if strings.Contains(idle, "Up:") {
		t.Fatalf("idle should not contain Up:, got %q", idle)
	}
	if strings.Contains(idle, " ms]") {
		t.Fatalf("idle should not contain ping, got %q", idle)
	}

	// session shows [ ·· ms] muted before first probe
	h.SetSession("shopee-2", "139.59.120.44", "vw:work", 0, true)
	text := h.Text()
	raw := h.RawText()
	if !strings.Contains(text, "[ ·· ms]") {
		t.Errorf("probing Text = %q, want [ ·· ms]", text)
	}
	if !strings.Contains(raw, "64748B") {
		t.Errorf("probing RawText = %q, want muted color 64748B", raw)
	}
	if strings.Contains(text, "Up:") {
		t.Errorf("should not contain Up: after ping, got %q", text)
	}
	// width always 8 chars: check ping slot substring
	if slot := extractPingSlot(text); slot != "[ ·· ms]" {
		t.Errorf("ping slot = %q, want [ ·· ms]", slot)
	}
	if len([]rune("[ ·· ms]")) != 8 {
		t.Errorf("probing slot rune len !=8")
	}

	// [ 42 ms] green
	h.SetPingResultForTest(42, true)
	text = h.Text()
	raw = h.RawText()
	if !strings.Contains(text, "[ 42 ms]") {
		t.Errorf("healthy Text = %q, want [ 42 ms]", text)
	}
	if !strings.Contains(raw, "22C55E") {
		t.Errorf("healthy RawText = %q, want green 22C55E", raw)
	}
	if slot := extractPingSlot(text); slot != "[ 42 ms]" {
		t.Errorf("healthy slot = %q, want [ 42 ms]", slot)
	}
	if len(slotBytes("[ 42 ms]")) != 8 {
		t.Errorf("healthy slot len !=8")
	}

	// [200 ms] amber
	h.SetPingResultForTest(200, true)
	text = h.Text()
	raw = h.RawText()
	if !strings.Contains(text, "[200 ms]") {
		t.Errorf("degraded Text = %q, want [200 ms]", text)
	}
	if !strings.Contains(raw, "F59E0B") {
		t.Errorf("degraded RawText = %q, want amber F59E0B", raw)
	}
	if slot := extractPingSlot(text); slot != "[200 ms]" {
		t.Errorf("degraded slot = %q", slot)
	}

	// [--- ms] red (dial error)
	h.SetPingResultForTest(0, false)
	text = h.Text()
	raw = h.RawText()
	if !strings.Contains(text, "[--- ms]") {
		t.Errorf("failed Text = %q, want [--- ms]", text)
	}
	if !strings.Contains(raw, "EF4444") {
		t.Errorf("failed RawText = %q, want red EF4444", raw)
	}
	if slot := extractPingSlot(text); slot != "[--- ms]" {
		t.Errorf("failed slot = %q", slot)
	}

	// clamp at 999
	h.SetPingResultForTest(1500, true)
	text = h.Text()
	if !strings.Contains(text, "[999 ms]") {
		t.Errorf("clamped Text = %q, want [999 ms]", text)
	}

	// width always 8 chars for all variants
	for _, want := range []string{"[ ·· ms]", "[ 42 ms]", "[200 ms]", "[--- ms]", "[999 ms]"} {
		if len([]rune(want)) != 8 {
			t.Errorf("slot %q rune len %d, want 8", want, len([]rune(want)))
		}
	}
	// also test 5ms => "[  5 ms]"
	h.SetPingResultForTest(5, true)
	text = h.Text()
	if !strings.Contains(text, "[  5 ms]") {
		t.Errorf("5ms Text = %q, want [  5 ms]", text)
	}

	// threshold boundary 120 => amber
	h.SetPingResultForTest(120, true)
	raw = h.RawText()
	if !strings.Contains(raw, "F59E0B") {
		t.Errorf("120ms should be amber, got %q", raw)
	}
	h.SetPingResultForTest(119, true)
	raw = h.RawText()
	if !strings.Contains(raw, "22C55E") {
		t.Errorf("119ms should be green, got %q", raw)
	}

	// stale-while-revalidate: re-setting same target keeps previous value, not probing
	h.SetDialFuncForTest(func(host, port string) (int, bool) { return 99, true })
	h.SetSession("shopee-2", "139.59.120.44", "vw:work", 0, true)
	if !strings.Contains(h.Text(), "[119 ms]") {
		t.Errorf("same target should keep stale [119 ms], got %q", h.Text())
	}
	if strings.Contains(h.Text(), "[ ·· ms]") {
		t.Errorf("same target should not reset to probing, got %q", h.Text())
	}

	h.Clear()
}

func TestSessionHeaderPingFollowsActiveSession(t *testing.T) {
	h := tviewui.NewSessionHeader()
	// track dial targets
	var dialTargets []string
	h.SetDialFuncForTest(func(host, port string) (int, bool) {
		time.Sleep(50 * time.Millisecond)
		dialTargets = append(dialTargets, host+":"+port)
		return 42, true
	})

	// first session
	h.SetSessionWithPort("host-a", "10.0.0.1", "22", "file", 0, true)
	if !strings.Contains(h.Text(), "[ ·· ms]") {
		t.Fatalf("after SetSession host-a, want probing, got %q", h.Text())
	}
	// simulate first probe completes
	h.SetPingResultForTest(42, true)
	if !strings.Contains(h.Text(), "[ 42 ms]") {
		t.Fatalf("after probe host-a, want [ 42 ms], got %q", h.Text())
	}
	targetA := h.PingTargetForTest()
	if targetA != "10.0.0.1:22" {
		t.Errorf("targetA = %q, want 10.0.0.1:22", targetA)
	}

	// switching active key resets to probing and retargets
	dialTargets = nil
	h.SetSessionWithPort("host-b", "10.0.0.2", "2222", "vw:work", 0, true)
	if !strings.Contains(h.Text(), "[ ·· ms]") {
		t.Errorf("after switch to host-b, want probing [ ·· ms], got %q", h.Text())
	}
	if strings.Contains(h.Text(), "[ 42 ms]") {
		t.Errorf("after switch, should not still show old 42ms, got %q", h.Text())
	}
	targetB := h.PingTargetForTest()
	if targetB != "10.0.0.2:2222" {
		t.Errorf("targetB = %q, want 10.0.0.2:2222", targetB)
	}
	// stale-while-revalidate check: before new probe, still probing
	// now simulate new probe
	h.SetPingResultForTest(200, true)
	if !strings.Contains(h.Text(), "[200 ms]") {
		t.Errorf("after probe host-b, want [200 ms], got %q", h.Text())
	}

	// switching back to host-a should reset again
	h.SetSessionWithPort("host-a", "10.0.0.1", "22", "file", 0, false)
	if !strings.Contains(h.Text(), "[ ·· ms]") {
		t.Errorf("switch back to host-a, want probing, got %q", h.Text())
	}

	// Clear stops probing
	h.Clear()
	if strings.Contains(h.Text(), " ms]") {
		t.Errorf("after Clear, should not contain ping, got %q", h.Text())
	}
	if h.PingTargetForTest() != "" {
		t.Errorf("after Clear target should be empty, got %q", h.PingTargetForTest())
	}

	// default port when empty -> 22
	h.SetSessionWithPort("host-c", "1.2.3.4", "", "file", 0, true)
	if got := h.PingTargetForTest(); got != "1.2.3.4:22" {
		t.Errorf("empty port target = %q, want 1.2.3.4:22", got)
	}
}

// helpers
func extractPingSlot(text string) string {
	// find "[ ... ms]" 8-char slot
	// look for " ms]" suffix
	idx := strings.LastIndex(text, " ms]")
	if idx < 0 {
		return ""
	}
	// slot starts 5 chars before " ms]"? Actually "[ ·· ms]" is 8 chars, so start = idx-4? Let's brute
	// text contains "[ 42 ms]" etc. Find '[' before idx.
	start := strings.LastIndex(text[:idx], "[")
	if start < 0 {
		return ""
	}
	end := idx + 4 // " ms]" = 4 chars? Actually " ms]" is 4
	if end > len(text) {
		end = len(text)
	}
	return text[start:end]
}

func slotBytes(s string) string {
	return s
}
