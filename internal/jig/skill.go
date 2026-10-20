package jig

import (
	"errors"
	"io/fs"
	"sort"
	"strings"
)

// Jig ships its own skill inside the binary and installs it in every
// workspace, so the skill that explains a command is the one the installed
// binary runs. It is a property of the binary, never declared in a schema:
// expandAgents prepends it to the skills sources of every schema, agents
// block or not, and a schema that declares the skills directory itself
// takes it in as a source (see addSkillsDerived).

// EmbeddedSkill is the skill pack, a directory holding jig/SKILL.md, set
// by main from the files embedded in the binary. Nil installs nothing,
// which is what tests and a build without the files get.
var EmbeddedSkill fs.FS

// EmbeddedSkillVersion names the jig version the pack came from; it is
// part of the pack's state key, so an upgraded binary rewrites the skill.
var EmbeddedSkillVersion string

const embeddedSkillName = "jig-skill"

func embeddedSkillSource() SrcEntry { return SrcEntry{embedded: embeddedSkillName} }

// embeddedSkillKey is the pack's source key in state.
func embeddedSkillKey() string {
	version := EmbeddedSkillVersion
	if version == "" {
		version = "dev"
	}
	return "embedded:" + embeddedSkillName + "@" + version
}

// EmbeddedSkillContent returns the skill file itself, for jig skill.
func EmbeddedSkillContent() ([]byte, error) {
	if EmbeddedSkill == nil {
		return nil, errors.New("this build of jig carries no embedded skill")
	}
	return fs.ReadFile(EmbeddedSkill, "jig/SKILL.md")
}

// fsDigest fingerprints an embedded tree the way localTreeDigest does a
// local directory: a hash over every file's path and content hash.
func fsDigest(fsys fs.FS) (string, error) {
	var lines []string
	err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		content, err := fs.ReadFile(fsys, path)
		if err != nil {
			return err
		}
		lines = append(lines, path+":"+sha256Hex(content))
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(lines)
	return sha256Hex([]byte(strings.Join(lines, "\n"))), nil
}

// materializeFS merges an embedded tree's files with the same rules as a
// local directory source.
func materializeFS(fsys fs.FS, merger *dirMerger) error {
	return fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		content, err := fs.ReadFile(fsys, path)
		if err != nil {
			return err
		}
		return merger.add(path, content, 0o644)
	})
}
