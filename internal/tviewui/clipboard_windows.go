//go:build windows

package tviewui

import (
	"errors"
	"runtime"
	"syscall"
	"unicode/utf16"
	"unsafe"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	procOpenClipboard    = user32.NewProc("OpenClipboard")
	procEmptyClipboard   = user32.NewProc("EmptyClipboard")
	procSetClipboardData = user32.NewProc("SetClipboardData")
	procCloseClipboard   = user32.NewProc("CloseClipboard")
	procGlobalAlloc      = kernel32.NewProc("GlobalAlloc")
	procGlobalLock       = kernel32.NewProc("GlobalLock")
	procGlobalUnlock     = kernel32.NewProc("GlobalUnlock")
	procGlobalFree       = kernel32.NewProc("GlobalFree")
	procLstrcpy          = kernel32.NewProc("lstrcpyW")
)

const (
	cfUnicodeText = 13
	gmemMoveable  = 0x0002
)

// copyToClipboard writes text to the Windows clipboard (CF_UNICODETEXT) using
// the Win32 API. Best-effort: on failure the selection is simply not copied.
func copyToClipboard(text string) error {
	if text == "" {
		return nil
	}
	// The clipboard belongs to the thread that opened it; keep the whole
	// open→write→close sequence on one OS thread to avoid a deadlock.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if r, _, _ := procOpenClipboard.Call(0); r == 0 {
		return errors.New("clipboard: open failed")
	}
	defer procCloseClipboard.Call()

	if r, _, _ := procEmptyClipboard.Call(0); r == 0 {
		return errors.New("clipboard: empty failed")
	}

	data := utf16.Encode([]rune(text))
	data = append(data, 0) // null terminator

	h, _, _ := procGlobalAlloc.Call(gmemMoveable, uintptr(len(data)*2))
	if h == 0 {
		return errors.New("clipboard: allocate failed")
	}
	defer func() {
		if h != 0 {
			procGlobalFree.Call(h)
		}
	}()

	p, _, _ := procGlobalLock.Call(h)
	if p == 0 {
		return errors.New("clipboard: lock failed")
	}
	if r, _, _ := procLstrcpy.Call(p, uintptr(unsafe.Pointer(&data[0]))); r == 0 {
		procGlobalUnlock.Call(h)
		return errors.New("clipboard: copy failed")
	}
	procGlobalUnlock.Call(h)

	if r, _, _ := procSetClipboardData.Call(cfUnicodeText, h); r == 0 {
		return errors.New("clipboard: set failed")
	}
	h = 0 // the clipboard now owns the memory
	return nil
}
