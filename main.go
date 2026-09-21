package main

import (
	"fmt"
	"os"

	"github.com/fvdsn/jig/internal/cli"
	"github.com/fvdsn/jig/internal/jig"
)

func main() {
	jig.ReleaseLocksOnInterrupt()
	if err := cli.Run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
