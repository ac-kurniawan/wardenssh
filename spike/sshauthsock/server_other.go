//go:build !windows

package sshauthsock

// On non-Windows platforms, the named-pipe spike is not applicable.
// Unix domain sockets are used instead (see internal/sshagent/listen_unix.go).
