//go:build windows

package jig

import (
	"errors"
	"syscall"
)

func isSymlinkPrivilegeError(err error) bool {
	return errors.Is(err, syscall.ERROR_PRIVILEGE_NOT_HELD)
}
