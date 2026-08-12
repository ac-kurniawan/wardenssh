#!/bin/bash
set -euo pipefail

echo "=== E2E Teardown ==="

# Stop VaultWarden on Local SBC
echo "[1/4] Stopping VaultWarden..."
ssh root@100.110.2.11 "docker rm -f vw-test 2>/dev/null; rm -rf /tmp/vw-data" || true

# Remove test user
echo "[2/4] Removing test user..."
userdel -r wardenssh-test 2>/dev/null || true

# Clean up keys and config
echo "[3/4] Cleaning up keys and config..."
rm -f /tmp/wardenssh-test-key /tmp/wardenssh-test-key.pub
rm -f /tmp/vault-setup /tmp/vault-setup.go /home/project/wardenssh/e2e/lib/vault-setup.go
rm -f /tmp/wardenssh /tmp/wardenssh-stderr.log
rm -f /tmp/e2e-env.sh
rm -f ~/.ssh/wardenssh.json
# Don't delete ~/.ssh/config — it might have user's real entries

# Kill any lingering tmux sessions
echo "[4/4] Killing tmux sessions..."
tmux kill-session -t ws 2>/dev/null || true
tmux kill-session -t ws2 2>/dev/null || true

echo "=== Teardown Complete ==="
