#!/bin/bash
set -euo pipefail
source e2e/lib/assert.sh
source e2e/lib/tmux-helpers.sh
source /tmp/e2e-env.sh

echo "=== Phase 4: TUI Interaction E2E ==="

# Start WardenSSH and login
echo "[1/8] Starting and logging in..."
tmux_start /tmp/wardenssh
sleep 2
tmux_type "$VW_PASS"
sleep 0.5
tmux_tab
sleep 0.3
tmux_enter
sleep 4

SCREEN=$(tmux_capture)
assert_contains "$SCREEN" "test-host" "Host list visible" || true

# Test fuzzy search — press / to enter search mode
echo "[2/8] Testing fuzzy search..."
tmux_type "/"
sleep 0.3
tmux_type "test"
sleep 0.5
SCREEN=$(tmux_capture)
assert_contains "$SCREEN" "test-host" "Search shows test-host" || true

# Clear search with Esc
echo "[3/8] Clearing search..."
tmux_escape
sleep 0.5

# Test scope cycling with Tab
echo "[4/8] Testing scope cycling (Tab)..."
tmux_tab
sleep 0.5
SCREEN=$(tmux_capture)
echo "  After first Tab:"
echo "$SCREEN" | head -3 | sed 's/^/    /'

tmux_tab
sleep 0.5
SCREEN=$(tmux_capture)
echo "  After second Tab:"
echo "$SCREEN" | head -3 | sed 's/^/    /'

# Test quit modal with q
echo "[5/8] Testing quit modal (q)..."
tmux_keys q
sleep 0.5
SCREEN=$(tmux_capture)
echo "  After q:"
echo "$SCREEN" | head -10 | sed 's/^/    /'

# If quit modal appeared (not exited), cancel it with Esc
if ! echo "$SCREEN" | grep -q "WARDENSSH_EXITED"; then
    echo "  Canceling quit modal..."
    tmux_escape
    sleep 0.5
    SCREEN=$(tmux_capture)
    assert_contains "$SCREEN" "test-host" "Back to host list after cancel" || true
fi

# Test Ctrl+Q
echo "[6/8] Testing Ctrl+Q..."
tmux_ctrl_q
sleep 0.5
SCREEN=$(tmux_capture)
echo "  After Ctrl+Q:"
echo "$SCREEN" | head -10 | sed 's/^/    /'

# Cancel again if modal appeared
if ! echo "$SCREEN" | grep -q "WARDENSSH_EXITED"; then
    tmux_escape
    sleep 0.5
fi

# Test connecting to test-host (navigate down) and then Esc to return
echo "[7/8] Testing connect then Esc..."
tmux_keys Down  # Move to test-host
sleep 0.3
tmux_enter  # Connect
sleep 3
tmux_escape  # Return to host list
sleep 1
SCREEN=$(tmux_capture)
assert_contains "$SCREEN" "test-host" "Esc returns to host list from session" || true

# Final cleanup
echo "[8/8] Cleaning up..."
tmux_type "exit" 2>/dev/null
tmux_enter 2>/dev/null
sleep 1
tmux_kill

print_summary
