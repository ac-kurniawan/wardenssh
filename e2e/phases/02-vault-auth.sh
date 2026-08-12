#!/bin/bash
set -euo pipefail
source e2e/lib/assert.sh
source e2e/lib/tmux-helpers.sh
source /tmp/e2e-env.sh

echo "=== Phase 2: VaultWarden Authentication E2E ==="

# Start WardenSSH
echo "[1/4] Starting WardenSSH..."
tmux_start /tmp/wardenssh
sleep 2
SCREEN=$(tmux_capture)

# Should show setup modal with Password field and Unlock/Skip buttons
echo "[2/4] Checking setup modal..."
assert_contains "$SCREEN" "Password" "Setup modal shows Password field" || true
assert_contains "$SCREEN" "Unlock" "Setup modal shows Unlock button" || true

# Type master password and press Enter (or Tab to Unlock then Enter)
echo "[3/4] Entering master password..."
tmux_type "$VW_PASS"
sleep 0.5
# Tab to the Unlock button and press Enter
tmux_tab
sleep 0.3
tmux_enter
sleep 4  # Wait for vault sync

SCREEN=$(tmux_capture)
echo "  Screen after login:"
echo "$SCREEN" | head -15 | sed 's/^/    /'

# Should now show the host list with our test-host
echo "[4/4] Verifying host list..."
assert_contains "$SCREEN" "test-host" "Host list shows test-host from vault" || true

# Clean up
tmux_kill

print_summary
