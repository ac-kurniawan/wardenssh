package tvxterm

import (
	"io"
)

// Backend is the transport abstraction for the terminal widget.
// Anything that can read terminal output, accept terminal input, and resize
// a terminal session can be attached.
type Backend interface {
	io.Reader
	io.Writer
	Resize(cols, rows int) error
	Close() error
}

// Upstream's NewPTYBackend (creack/pty, Unix-only) is intentionally omitted
// from this vendored copy: WardenSSH uses its own cross-platform backend
// (internal/tviewui.PtyBackend over github.com/aymanbagabas/go-pty, ConPTY on
// Windows). Keeping the Unix-only helper would re-introduce a creack/pty
// dependency and fail the Windows test run.
