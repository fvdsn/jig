//go:build !windows

package jig

func isSymlinkPrivilegeError(error) bool {
	return false
}
