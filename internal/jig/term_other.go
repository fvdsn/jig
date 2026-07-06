//go:build !(darwin || linux)

package jig

import "os"

func termWidth(_ *os.File) int {
	return 0
}
