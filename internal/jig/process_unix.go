//go:build !windows

package jig

import (
	"errors"
	"syscall"
)

// processAlive reports whether a process with the pid exists. Signal 0
// delivers nothing but still checks the target; a permission error means
// the process exists but belongs to another user.
func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
