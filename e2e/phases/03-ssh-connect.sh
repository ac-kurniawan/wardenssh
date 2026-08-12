#!/bin/bash
set -euo pipefail
source e2e/lib/assert.sh
source e2e/lib/tmux-helpers.sh
source /tmp/e2e-env.sh

echo "=== Phase 3: SSH Connection via Agent E2E ==="

# Start WardenSSH
echo "[1/6] Starting WardenSSH..."
tmux_start /tmp/wardenssh
sleep 2

# Login to vault — type password, Tab to Unlock, Enter
echo "[2/6] Logging into vault..."
tmux_type "$VW_PASS"
sleep 0.5
tmux_enter
sleep 5

SCREEN=$(tmux_capture)
assert_contains "$SCREEN" "test-host" "Host list shows test-host" || true

# Navigate down to test-host (file-host is first, test-host is second)
echo "[3/6] Connecting to test-host..."
tmux_keys Down  # Move to test-host
sleep 0.3
tmux_enter  # Connect to selected host
sleep 3

SCREEN=$(tmux_capture)
echo "  Screen after connect:"
echo "$SCREEN" | head -15 | sed 's/^/    /'

# Should show remote shell prompt
echo "[4/6] Verifying SSH connection..."
assert_match "$SCREEN" "wardenssh-test|\\$|#" "SSH session active (shell prompt visible)" || true

# Run a command in the SSH session
echo "[5/6] Running command in SSH session..."
tmux_type "whoami"
tmux_enter
sleep 1

SCREEN=$(tmux_capture)
assert_contains "$SCREEN" "wardenssh-test" "whoami returns wardenssh-test" || true

# Exit SSH session
echo "[6/6] Exiting SSH session..."
tmux_type "exit"
tmux_enter
sleep 2

SCREEN=$(tmux_capture)
# Should return to host list
assert_contains "$SCREEN" "test-host" "Returned to host list after SSH exit" || true

tmux_kill

print_summary
