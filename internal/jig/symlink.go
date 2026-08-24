package jig

import (
	"fmt"
	"os"
)

// makeSymlink creates a symlink, decorating the Windows privilege error with
// its remedy: without Developer Mode or an elevated shell, Windows refuses
// symlink creation with a bare "required privilege is not held".
func makeSymlink(target string, link string) error {
	err := os.Symlink(target, link)
	if err != nil && isSymlinkPrivilegeError(err) {
		return fmt.Errorf("%w; creating symlinks on Windows requires Developer Mode (Settings > System > For developers) or an administrator shell", err)
	}
	return err
}
