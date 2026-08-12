#!/bin/bash
# tmux helpers for driving the WardenSSH TUI headlessly

TMUX_SESSION="${TMUX_SESSION:-ws}"
TMUX_WIDTH="${TMUX_WIDTH:-120}"
TMUX_HEIGHT="${TMUX_HEIGHT:-40}"

# Start WardenSSH in a detached tmux session
tmux_start() {
    local bin="$1"
    shift
    tmux kill-session -t "$TMUX_SESSION" 2>/dev/null || true
    tmux new-session -d -s "$TMUX_SESSION" -x "$TMUX_WIDTH" -y "$TMUX_HEIGHT" \
        "TERM=xterm-256color $bin $* 2>/tmp/wardenssh-stderr.log; echo 'WARDENSSH_EXITED' ; sleep 30"
    sleep 1
}

# Capture the current screen content
tmux_capture() {
    tmux capture-pane -t "$TMUX_SESSION" -p
}

# Send keys to the tmux session
tmux_keys() {
    tmux send-keys -t "$TMUX_SESSION" "$@"
}

# Send a string (types each character)
tmux_type() {
    tmux send-keys -t "$TMUX_SESSION" -l "$1"
}

# Send Enter key
tmux_enter() {
    tmux send-keys -t "$TMUX_SESSION" Enter
}

# Send Escape key
tmux_escape() {
    tmux send-keys -t "$TMUX_SESSION" Escape
}

# Send Tab key
tmux_tab() {
    tmux send-keys -t "$TMUX_SESSION" Tab
}

# Send Ctrl+Q
tmux_ctrl_q() {
    tmux send-keys -t "$TMUX_SESSION" C-q
}

# Send Ctrl+C
tmux_ctrl_c() {
    tmux send-keys -t "$TMUX_SESSION" C-c
}

# Wait and capture
tmux_wait_capture() {
    local secs="${1:-2}"
    sleep "$secs"
    tmux_capture
}

# Kill the tmux session
tmux_kill() {
    tmux kill-session -t "$TMUX_SESSION" 2>/dev/null || true
}

# Wait for text to appear on screen (polls every 200ms, up to timeout)
tmux_wait_for() {
    local needle="$1"
    local timeout="${2:-10}"
    local count=$((timeout * 5))
    local i=0
    while [ "$i" -lt "$count" ]; do
        if tmux_capture | grep -qF "$needle"; then
            return 0
        fi
        sleep 0.2
        i=$((i + 1))
    done
    return 1
}
