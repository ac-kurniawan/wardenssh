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
tmux_type "$VW_PASS"
sleep 0.5
tmux_tab
sleep 0.3
tmux_enter
sleep 4

SCREEN=$(tmux_capture)
assert_contains "$SCREEN" "test-host" "Host list shows test-host" || true
assert_contains "$SCREEN" "file-host" "Host list shows file-host" || true

# Connect to file-host (it's first in the list, already selected)
echo "[2/8] Connecting to file-host..."
tmux_enter  # Connect to file-host (first in list)
sleep 3
SCREEN=$(tmux_capture)
assert_match "$SCREEN" "wardenssh-test|\\$" "SSH session 1 active" || true

# Esc back to host list (yield-and-switch)
echo "[3/8] Returning to host list (yield)..."
tmux_escape
sleep 1
SCREEN=$(tmux_capture)
assert_contains "$SCREEN" "file-host" "Back to host list" || true

# Connect to test-host (navigate down to it)
echo "[4/8] Connecting to test-host..."
tmux_keys Down  # Move to test-host
sleep 0.3
tmux_enter  # Connect
sleep 3
SCREEN=$(tmux_capture)
assert_match "$SCREEN" "wardenssh-test|\\$" "SSH session 2 active" || true

# Esc back to host list — both should be live
echo "[5/8] Returning to host list..."
tmux_escape
sleep 1
SCREEN=$(tmux_capture)
echo "  Host list with both live:"
echo "$SCREEN" | head -10 | sed 's/^/    /'
assert_contains "$SCREEN" "test-host" "test-host in list" || true
assert_contains "$SCREEN" "file-host" "file-host in list" || true

# Quit modal should warn about live sessions
echo "[6/8] Testing quit with live sessions..."
tmux_keys q
sleep 0.5
SCREEN=$(tmux_capture)
echo "  Quit modal:"
echo "$SCREEN" | head -10 | sed 's/^/    /'
# Should NOT have exited immediately (live sessions exist)
assert_not_contains "$SCREEN" "WARDENSSH_EXITED" "Quit modal appeared (not immediate exit)" || true

# Kill all sessions — try Enter to confirm
echo "[7/8] Killing all sessions..."
tmux_enter
sleep 2
SCREEN=$(tmux_capture)

echo "[8/8] Cleaning up..."
tmux_kill

print_summary
