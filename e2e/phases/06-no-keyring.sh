#!/bin/bash
set -euo pipefail
source e2e/lib/assert.sh
source e2e/lib/tmux-helpers.sh
source /tmp/e2e-env.sh

echo "=== Phase 6: --no-keyring Mode E2E ==="

# Start WardenSSH with --no-keyring
echo "[1/4] Starting WardenSSH with --no-keyring..."
tmux_start /tmp/wardenssh --no-keyring
sleep 2
SCREEN=$(tmux_capture)

# Should show setup modal (NOT auto-logged in)
echo "[2/4] Verifying manual password prompt..."
echo "  Screen:"
echo "$SCREEN" | head -15 | sed 's/^/    /'

# Should show the setup form with Password field and Unlock button
assert_contains "$SCREEN" "Password" "Setup modal visible with Password field" || true
assert_contains "$SCREEN" "Unlock" "Unlock button visible" || true

# Type master password manually
echo "[3/4] Entering master password manually..."
tmux_type "$VW_PASS"
sleep 0.5
tmux_enter
sleep 5

SCREEN=$(tmux_capture)
assert_contains "$SCREEN" "test-host" "Host list appears after manual login" || true

echo "[4/4] Cleaning up..."
tmux_kill

print_summary
