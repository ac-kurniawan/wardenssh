#!/bin/bash
set -euo pipefail

export PATH=$PATH:/usr/local/go/bin
cd /home/project/wardenssh

VW_URL="http://100.110.2.11:8000"
VW_EMAIL="wardenssh-test@example.com"
VW_PASS="TestMasterPass123!"
SSH_TARGET_IP="100.64.213.54"  # This server's Tailscale IP
SSH_TARGET_USER="wardenssh-test"
SSH_TARGET_PORT="22"

echo "=== E2E Setup ==="

# 1. Start VaultWarden container on Local SBC
echo "[1/6] Starting VaultWarden on Local SBC..."
ssh root@100.110.2.11 "docker rm -f vw-test 2>/dev/null; rm -rf /tmp/vw-data; mkdir -p /tmp/vw-data && docker run -d --name vw-test -e ADMIN_TOKEN=test-admin-token -v /tmp/vw-data:/data -p 8000:80 --restart unless-stopped vaultwarden/server:latest"

# 2. Wait for VaultWarden to be ready
echo "[2/6] Waiting for VaultWarden..."
for i in $(seq 1 30); do
    if ssh root@100.110.2.11 "curl -sf http://localhost:8000/alive" >/dev/null 2>&1; then
        echo "  VaultWarden is ready"
        break
    fi
    sleep 1
done

# 3. Generate test SSH keypair
echo "[3/6] Generating test SSH key..."
rm -f /tmp/wardenssh-test-key /tmp/wardenssh-test-key.pub
ssh-keygen -t ed25519 -f /tmp/wardenssh-test-key -N "" -C "wardenssh-e2e-test"
echo "  Key generated: /tmp/wardenssh-test-key"

# 4. Create test user on this server (SSH target)
echo "[4/6] Creating test user $SSH_TARGET_USER..."
if ! id "$SSH_TARGET_USER" &>/dev/null; then
    useradd -m -s /bin/bash "$SSH_TARGET_USER"
fi
mkdir -p /home/$SSH_TARGET_USER/.ssh
cp /tmp/wardenssh-test-key.pub /home/$SSH_TARGET_USER/.ssh/authorized_keys
chmod 700 /home/$SSH_TARGET_USER/.ssh
chmod 600 /home/$SSH_TARGET_USER/.ssh/authorized_keys
chown -R "$SSH_TARGET_USER":"$SSH_TARGET_USER" /home/$SSH_TARGET_USER/.ssh
echo "  User created, key installed"

# 5. Register vault account + create SSH-Key item
echo "[5/6] Registering vault account and creating SSH-Key item..."
source e2e/lib/vault-api.sh
build_vault_helper

# Register (may fail if account exists — that's ok)
/tmp/vault-setup register "$VW_URL" "$VW_EMAIL" "$VW_PASS" 2>/dev/null || echo "  Account already exists, continuing..."

# Create SSH-Key item in the vault
/tmp/vault-setup create-item "$VW_URL" "$VW_EMAIL" "$VW_PASS" \
    "test-host" "$SSH_TARGET_IP" "$SSH_TARGET_USER" "$SSH_TARGET_PORT" \
    /tmp/wardenssh-test-key

# Verify login + sync works
/tmp/vault-setup login-test "$VW_URL" "$VW_EMAIL" "$VW_PASS"

# 6. Write wardenssh config
echo "[6/6] Writing wardenssh config..."
mkdir -p ~/.ssh
cat > ~/.ssh/wardenssh.json << EOF
{
    "vaults": [
        {
            "name": "test",
            "server": "$VW_URL",
            "email": "$VW_EMAIL"
        }
    ],
    "custom_fields": {
        "host": "host",
        "user": "user",
        "port": "port",
        "proxyjump": "proxyjump"
    },
    "keyring": false
}
EOF
echo "  Config written to ~/.ssh/wardenssh.json"

# Write env file for other scripts
cat > /tmp/e2e-env.sh << EOF
export VW_URL="$VW_URL"
export VW_EMAIL="$VW_EMAIL"
export VW_PASS="$VW_PASS"
export SSH_TARGET_IP="$SSH_TARGET_IP"
export SSH_TARGET_USER="$SSH_TARGET_USER"
export SSH_TARGET_PORT="$SSH_TARGET_PORT"
export WARDENSSH_BIN="/tmp/wardenssh"
export TEST_KEY="/tmp/wardenssh-test-key"
EOF

# Also add a file-sourced host to ~/.ssh/config for offline tests
cat > ~/.ssh/config << 'EOF'
Host file-host
    HostName 100.64.213.54
    User wardenssh-test
    Port 22
    IdentityFile /tmp/wardenssh-test-key
EOF
chmod 600 ~/.ssh/config

echo ""
echo "=== Setup Complete ==="
echo "Run: source /tmp/e2e-env.sh && e2e/run-all.sh"
