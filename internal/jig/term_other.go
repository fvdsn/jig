//go:build !(darwin || linux)

package jig

import "os"

func termWidth(_ *os.File) int {
	return 0
}

// isTerminal falls back to the character-device check where the ioctl is
// unavailable; consoles are character devices, pipes and files are not.
func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
