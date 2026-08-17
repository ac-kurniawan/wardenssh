#!/bin/bash
set -euo pipefail
source e2e/lib/assert.sh
source e2e/lib/tmux-helpers.sh
source /tmp/e2e-env.sh

echo "=== Phase 5: Multi-Session E2E ==="

# Start WardenSSH and login
echo "[1/8] Starting and logging in..."
tmux_start /tmp/wardenssh
sleep 2
SCREEN=$(tmux_capture)
if echo "$SCREEN" | grep -q "Password"; then
    tmux_type "$VW_PASS"
    sleep 0.5
    tmux_enter
    sleep 5
fi

SCREEN=$(tmux_capture)
assert_contains "$SCREEN" "test-host" "Host list shows test-host" || true
assert_contains "$SCREEN" "file-host" "Host list shows file-host" || true

# Connect to file-host (first in list, already selected)
echo "[2/8] Connecting to file-host..."
tmux_enter
sleep 3
SCREEN=$(tmux_capture)
assert_match "$SCREEN" "wardenssh-test|\\$" "SSH session 1 (file-host) active" || true

# Ctrl+B back to host list (Esc is forwarded to the terminal)
echo "[3/8] Returning to host list (yield)..."
tmux_ctrl_b
sleep 1
SCREEN=$(tmux_capture)
assert_contains "$SCREEN" "file-host" "Back to host list" || true

# Connect to test-host (navigate down)
echo "[4/8] Connecting to test-host..."
tmux_keys Down
sleep 0.5
tmux_enter
sleep 3
SCREEN=$(tmux_capture)
assert_match "$SCREEN" "wardenssh-test|\\$" "SSH session 2 (test-host) active" || true

# Ctrl+B back to host list — both should be live (green dots)
echo "[5/8] Returning to host list..."
tmux_ctrl_b
sleep 1
SCREEN=$(tmux_capture)
echo "  Host list with both live:"
echo "$SCREEN" | head -6 | sed 's/^/    /'
assert_contains "$SCREEN" "test-host" "test-host in list" || true
assert_contains "$SCREEN" "file-host" "file-host in list" || true

# Quit modal should warn about live sessions
echo "[6/8] Testing quit with live sessions..."
tmux_keys q
sleep 0.5
SCREEN=$(tmux_capture)
echo "  Quit modal:"
echo "$SCREEN" | head -10 | sed 's/^/    /'
assert_not_contains "$SCREEN" "WARDENSSH_EXITED" "Quit modal appeared (not immediate exit)" || true

# Kill all — look for Kill All text and press Enter
echo "[7/8] Killing all sessions..."
# The quit modal has "Kill All" and "Detach" options. Try Enter to confirm.
tmux_enter
sleep 2
SCREEN=$(tmux_capture)

echo "[8/8] Cleaning up..."
tmux_kill

print_summary
