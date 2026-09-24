//go:build windows

package session

// ProcessAlive reports whether pid is still running. Windows session close
// does not use Unix process groups; this exists so tests can share the name.
func ProcessAlive(pid int) bool {
	return false
}
