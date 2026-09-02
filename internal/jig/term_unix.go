//go:build darwin || linux

package jig

import (
	"os"
	"syscall"
	"unsafe"
)

// termWidth returns the column width of the terminal behind f, or 0 when f
// is not a terminal.
func termWidth(f *os.File) int {
	var size struct{ rows, cols, x, y uint16 }
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), syscall.TIOCGWINSZ, uintptr(unsafe.Pointer(&size)))
	if errno != 0 {
		return 0
	}
	return int(size.cols)
}

// isTerminal reports whether f is attached to a terminal — distinct from
// termWidth, since a terminal may report zero columns.
func isTerminal(f *os.File) bool {
	var size struct{ rows, cols, x, y uint16 }
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), syscall.TIOCGWINSZ, uintptr(unsafe.Pointer(&size)))
	return errno == 0
}
