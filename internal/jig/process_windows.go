//go:build windows

package jig

import (
	"errors"
	"syscall"
)

const (
	processQueryLimitedInformation = 0x1000
	stillActive                    = 259
	errorInvalidParameter          = syscall.Errno(87)
)

// processAlive reports whether a process with the pid exists. OpenProcess
// rejects an unknown pid with an invalid-parameter error; any other failure
// (access denied on an elevated process, say) means it exists. An exited
// process whose handle is still held somewhere opens fine but no longer
// reports the still-active exit code.
func processAlive(pid int) bool {
	handle, err := syscall.OpenProcess(processQueryLimitedInformation, false, uint32(pid))
	if err != nil {
		return !errors.Is(err, errorInvalidParameter)
	}
	defer func() { _ = syscall.CloseHandle(handle) }()
	var code uint32
	if err := syscall.GetExitCodeProcess(handle, &code); err != nil {
		return true
	}
	return code == stillActive
}
