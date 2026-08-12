#!/bin/bash
set -uo pipefail

export PATH=$PATH:/usr/local/go/bin
cd /home/project/wardenssh

echo "╔══════════════════════════════════════════╗"
echo "║   WardenSSH E2E Test Suite               ║"
echo "╚══════════════════════════════════════════╝"
echo ""

# Source env
if [ ! -f /tmp/e2e-env.sh ]; then
    echo "❌ E2E environment not set up. Run e2e/setup.sh first."
    exit 1
fi
source /tmp/e2e-env.sh

TOTAL_PASS=0
TOTAL_FAIL=0
PHASES_RUN=0
PHASES_FAILED=()

run_phase() {
    local phase="$1"
    local name="$2"
    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "  Running: $name"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    
    local output
    output=$(bash "$phase" 2>&1)
    echo "$output"
    
    # Extract pass/fail counts from the summary line
    local summary
    summary=$(echo "$output" | grep "Results:" | tail -1)
    local p f
    p=$(echo "$summary" | grep -oP '\d+(?= passed)' || echo 0)
    f=$(echo "$summary" | grep -oP '\d+(?= failed)' || echo 0)
    
    TOTAL_PASS=$((TOTAL_PASS + p))
    TOTAL_FAIL=$((TOTAL_FAIL + f))
    PHASES_RUN=$((PHASES_RUN + 1))
    
    if [ "$f" -gt 0 ]; then
        PHASES_FAILED+=("$name")
    fi
}

run_phase "e2e/phases/01-build.sh" "Phase 1: Build & Binary Smoke"
run_phase "e2e/phases/02-vault-auth.sh" "Phase 2: VaultWarden Auth"
run_phase "e2e/phases/03-ssh-connect.sh" "Phase 3: SSH Connection via Agent"
run_phase "e2e/phases/04-tui.sh" "Phase 4: TUI Interaction"
run_phase "e2e/phases/05-multi-session.sh" "Phase 5: Multi-Session"
run_phase "e2e/phases/06-no-keyring.sh" "Phase 6: --no-keyring Mode"
run_phase "e2e/phases/07-offline.sh" "Phase 7: Offline Mode"

echo ""
echo "╔══════════════════════════════════════════╗"
echo "║   E2E Test Suite Complete                 ║"
echo "╠══════════════════════════════════════════╣"
echo "║  Phases run:    $PHASES_RUN                        ║"
echo "║  Total passes:  $TOTAL_PASS                        ║"
echo "║  Total failures: $TOTAL_FAIL                        ║"
if [ ${#PHASES_FAILED[@]} -gt 0 ]; then
    echo "║  Failed phases:                           ║"
    for name in "${PHASES_FAILED[@]}"; do
        echo "║    - $name                      ║"
    done
fi
echo "╚══════════════════════════════════════════╝"

exit $TOTAL_FAIL
