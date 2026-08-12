#!/bin/bash
set -euo pipefail
source e2e/lib/assert.sh
source e2e/lib/tmux-helpers.sh
source /tmp/e2e-env.sh

echo "=== Phase 7: Offline Mode E2E ==="

# Stop VaultWarden
echo "[1/4] Stopping VaultWarden..."
ssh root@100.110.2.11 "docker stop vw-test" 2>/dev/null || true
sleep 2

# Start WardenSSH
echo "[2/4] Starting WardenSSH (vault offline)..."
tmux_start /tmp/wardenssh
sleep 3
SCREEN=$(tmux_capture)
echo "  Screen:"
echo "$SCREEN" | head -15 | sed 's/^/    /'

# Should show error or degraded mode, but file-host from ~/.ssh/config should appear
echo "[3/4] Verifying offline behavior..."
# The setup modal may show a sync error, or the host list may show only file hosts
# Try to get past the setup modal (press Escape or Enter to skip)
tmux_escape
sleep 1
SCREEN=$(tmux_capture)
echo "  After Escape:"
echo "$SCREEN" | head -15 | sed 's/^/    /'

# Should see file-host from ~/.ssh/config
assert_contains "$SCREEN" "file-host" "File-host visible in offline mode" || true

echo "[4/4] Cleaning up..."
tmux_kill

# Restart VaultWarden for subsequent tests
ssh root@100.110.2.11 "docker start vw-test" 2>/dev/null || true
sleep 3

print_summary
