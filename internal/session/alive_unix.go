//go:build !windows

package session

import (
	"os"
	"strconv"
	"strings"
	"syscall"
)

// ProcessAlive reports whether pid is a live process. A zombie is not alive:
// kill(pid, 0) still succeeds for a reaped-but-not-waited child, which would
// hide descendants that survived a session kill.
func ProcessAlive(pid int) bool {
	if pid <= 1 {
		return false
	}
	if err := syscall.Kill(pid, 0); err != nil {
		return false
	}
	state, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		// No procfs (macOS). kill(0) is the best portable signal; a zombie
		// there is reaped by launchd/init immediately when the parent died
		// without waiting, so a surviving kill(0) means a live process.
		return true
	}
	// stat is "pid (comm) state ..."; comm may contain spaces and parens.
	s := string(state)
	i := strings.LastIndex(s, ")")
	if i < 0 || i+2 >= len(s) {
		return true
	}
	return s[i+2] != 'Z'
}
