#!/bin/bash
# Assertion helpers for E2E tests

PASS=0
FAIL=0

assert_contains() {
    local haystack="$1"
    local needle="$2"
    local label="${3:-assert}"
    if echo "$haystack" | grep -qF "$needle"; then
        echo "  ✅ PASS: $label (found '$needle')"
        PASS=$((PASS + 1))
    else
        echo "  ❌ FAIL: $label (expected '$needle' in output)"
        echo "    Output was:"
        echo "$haystack" | head -20 | sed 's/^/      /'
        FAIL=$((FAIL + 1))
    fi
}

assert_not_contains() {
    local haystack="$1"
    local needle="$2"
    local label="${3:-assert}"
    if echo "$haystack" | grep -qF "$needle"; then
        echo "  ❌ FAIL: $label (did not expect '$needle' in output)"
        FAIL=$((FAIL + 1))
    else
        echo "  ✅ PASS: $label ('$needle' not present)"
        PASS=$((PASS + 1))
    fi
}

assert_match() {
    local haystack="$1"
    local pattern="$2"
    local label="${3:-assert}"
    if echo "$haystack" | grep -qE "$pattern"; then
        echo "  ✅ PASS: $label (matched '$pattern')"
        PASS=$((PASS + 1))
    else
        echo "  ❌ FAIL: $label (expected pattern '$pattern')"
        FAIL=$((FAIL + 1))
    fi
}

print_summary() {
    echo ""
    echo "================================"
    echo "  Results: $PASS passed, $FAIL failed"
    echo "================================"
    return $FAIL
}
