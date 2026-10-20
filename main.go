package main

import (
	"embed"
	"fmt"
	"io/fs"
	"os"

	"github.com/fvdsn/jig/internal/cli"
	"github.com/fvdsn/jig/internal/jig"
)

// skillFiles is jig's own skill, installed in every workspace (see
// internal/jig/skill.go).
//
//go:embed .agents/skills/jig
var skillFiles embed.FS

func main() {
	skills, err := fs.Sub(skillFiles, ".agents/skills")
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	jig.EmbeddedSkill = skills
	jig.ReleaseLocksOnInterrupt()
	if err := cli.Run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
