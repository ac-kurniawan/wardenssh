#!/bin/bash
set -euo pipefail
source e2e/lib/assert.sh

echo "=== Phase 1: Build & Binary Smoke ==="

export PATH=$PATH:/usr/local/go/bin
cd /home/project/wardenssh

# Build the binary
echo "[1/3] Building wardenssh..."
go build -o /tmp/wardenssh ./cmd/wardenssh 2>&1
assert_contains "build exit $?" "0" "go build succeeds" || true

# Verify binary exists and is executable
if [ -x /tmp/wardenssh ]; then
    echo "  ✅ PASS: binary exists and is executable"
    PASS=$((PASS + 1))
else
    echo "  ❌ FAIL: binary not found or not executable"
    FAIL=$((FAIL + 1))
fi

# Start in tmux and verify it doesn't crash immediately
echo "[2/3] Starting WardenSSH in tmux..."
source e2e/lib/tmux-helpers.sh
tmux_start /tmp/wardenssh
sleep 2
SCREEN=$(tmux_capture)

# TUI shows "Hosts" header and a setup modal with Password/Unlock/Skip
assert_contains "$SCREEN" "Hosts" "TUI shows Hosts header" || true
assert_contains "$SCREEN" "Unlock" "TUI shows setup modal with Unlock button" || true
if [ -n "$SCREEN" ]; then
    echo "  ✅ PASS: TUI rendered (non-empty screen)"
    PASS=$((PASS + 1))
else
    echo "  ❌ FAIL: TUI screen is empty"
    FAIL=$((FAIL + 1))
fi

# Quit cleanly — use Ctrl+C which should trigger quit
echo "[3/3] Quitting..."
tmux_ctrl_c
sleep 1
SCREEN=$(tmux_capture)
# Check if exited or if quit modal appeared
if echo "$SCREEN" | grep -q "WARDENSSH_EXITED"; then
    echo "  ✅ PASS: WardenSSH exited cleanly"
    PASS=$((PASS + 1))
else
    # Try q key as fallback
    tmux_keys q
    sleep 1
    SCREEN=$(tmux_capture)
    if echo "$SCREEN" | grep -q "WARDENSSH_EXITED"; then
        echo "  ✅ PASS: WardenSSH exited cleanly (via q)"
        PASS=$((PASS + 1))
    else
        echo "  ❌ FAIL: WardenSSH did not exit"
        FAIL=$((FAIL + 1))
    fi
fi

tmux_kill

print_summary
