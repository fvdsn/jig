package jig

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

type StatusOptions struct {
	Path            string
	IncludeArchived bool
	Tags            []string
}

// Status glyphs. Each line carries the most significant glyph plus a note that
// spells out every applicable state, so the glyphs never need to be read alone.
const (
	glyphClean    = "✓" // ✓ present, tracked, in sync
	glyphDirty    = "●" // ● uncommitted changes / modified file
	glyphRemote   = "⇄" // ⇄ origin differs from the definition
	glyphMoved    = "→" // → checkout lives at a different path
	glyphMissing  = "✗" // ✗ defined but not present
	glyphConflict = "⚠" // ⚠ present but not what jig expects
	glyphAhead    = "↑" // ↑ commits not pushed to upstream
	glyphBehind   = "↓" // ↓ upstream has commits not pulled
	glyphDiverged = "⇅" // ⇅ ahead and behind at the same time
)

type statusLine struct {
	glyph  string
	path   string
	branch string
	note   string
}

func Status(options StatusOptions, out io.Writer) error {
	ws, err := loadWorkspace(false)
	if err != nil {
		return err
	}
	query := NodeQuery{Path: options.Path, IncludeArchived: options.IncludeArchived, Tags: options.Tags}
	selection, err := ws.Select(query)
	if err != nil {
		return err
	}

	installedNodes := ws.installedNodes()
	activeFiles := activeFilesForRepoSet(&ws.Model, map[string]bool{}, installedNodes.Repos, installedNodes.Files, options.IncludeArchived)

	type result struct {
		line statusLine
		ok   bool
	}
	results := make([]result, len(selection.Entries))
	forEachParallel(len(selection.Entries), func(i int) {
		entry := selection.Entries[i]
		switch entry.Kind {
		case EntryRepo:
			results[i] = result{repoStatusLine(ws, entry), true}
		case EntryFile:
			line, ok := fileStatusLine(ws, entry, activeFiles)
			results[i] = result{line, ok}
		}
	})
	var lines []statusLine
	for _, r := range results {
		if r.ok {
			lines = append(lines, r.line)
		}
	}

	if selection.Path == "" && len(options.Tags) == 0 {
		lines = append(lines, staleStatusLines(ws)...)
	}

	printStatusLines(out, lines)
	return nil
}

func repoStatusLine(ws *Workspace, entry Entry) statusLine {
	repoPath := entry.Path
	expectedAbs := filepath.Join(ws.Root, entry.Path)

	if stateRepo, ok := ws.State.Repos[entry.Identity]; ok && stateRepo.Path != entry.Path {
		oldAbs := filepath.Join(ws.Root, stateRepo.Path)
		if isGitRepo(oldAbs) {
			note := "moved from " + stateRepo.Path
			if isDirty(oldAbs) {
				note += ", dirty"
			}
			return statusLine{glyphMoved, repoPath, gitBranch(oldAbs), note}
		}
	}
	if !pathExists(expectedAbs) {
		return statusLine{glyphMissing, repoPath, "", "missing"}
	}
	if !isGitRepo(expectedAbs) {
		return statusLine{glyphConflict, repoPath, "", "not a git repository"}
	}

	branch := gitBranch(expectedAbs)
	glyph := glyphClean
	var notes []string
	if origin, err := gitOrigin(expectedAbs); err != nil || origin != entry.Repo.Git {
		glyph = glyphRemote
		notes = append(notes, "remote-changed")
	}
	if isDirty(expectedAbs) {
		if glyph == glyphClean {
			glyph = glyphDirty
		}
		notes = append(notes, "dirty")
	}
	if ahead, behind, ok := aheadBehind(expectedAbs); ok && ahead+behind > 0 {
		if glyph == glyphClean {
			switch {
			case ahead > 0 && behind > 0:
				glyph = glyphDiverged
			case ahead > 0:
				glyph = glyphAhead
			default:
				glyph = glyphBehind
			}
		}
		if ahead > 0 {
			notes = append(notes, fmt.Sprintf("ahead %d", ahead))
		}
		if behind > 0 {
			notes = append(notes, fmt.Sprintf("behind %d", behind))
		}
	}
	return statusLine{glyph, repoPath, branch, strings.Join(notes, ", ")}
}

func fileStatusLine(ws *Workspace, entry Entry, activeFiles map[string]bool) (statusLine, bool) {
	filePath := entry.Path
	if !activeFiles[filePath] {
		return statusLine{}, false
	}
	expectedAbs := filepath.Join(ws.Root, entry.Path)
	stateFile, hasState := ws.State.Files[entry.Identity]

	if hasState && stateFile.Path != entry.Path && pathExists(filepath.Join(ws.Root, stateFile.Path)) {
		return statusLine{glyphMoved, filePath, "", "moved from " + stateFile.Path}, true
	}
	if !pathExists(expectedAbs) {
		return statusLine{glyphMissing, filePath, "", "missing"}, true
	}
	if !hasState {
		return statusLine{glyphConflict, filePath, "", "untracked"}, true
	}
	if entry.File.Link != "" {
		return symlinkStatusLine(ws, entry, expectedAbs), true
	}
	currentHash, err := fileSHA256(expectedAbs)
	if err != nil {
		return statusLine{glyphConflict, filePath, "", err.Error()}, true
	}
	if currentHash != stateFile.SHA256 {
		return statusLine{glyphDirty, filePath, "", "modified"}, true
	}
	return statusLine{glyphClean, filePath, "", ""}, true
}

func symlinkStatusLine(ws *Workspace, entry Entry, expectedAbs string) statusLine {
	filePath := entry.Path
	info, err := os.Lstat(expectedAbs)
	if err != nil {
		return statusLine{glyphConflict, filePath, "", err.Error()}
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return statusLine{glyphConflict, filePath, "", "not a symlink"}
	}
	targetEntry, _ := ws.Model.entry(entry.File.Link, EntryFile)
	expectedTarget, err := relativeSymlinkTarget(entry.Path, targetEntry.Path)
	if err != nil {
		return statusLine{glyphConflict, filePath, "", err.Error()}
	}
	currentTarget, err := os.Readlink(expectedAbs)
	if err != nil {
		return statusLine{glyphConflict, filePath, "", err.Error()}
	}
	if currentTarget != expectedTarget {
		return statusLine{glyphDirty, filePath, "", "modified"}
	}
	return statusLine{glyphClean, filePath, "", ""}
}

func staleStatusLines(ws *Workspace) []statusLine {
	var lines []statusLine
	repoPaths := repoIdentityToPath(&ws.Model)
	for identity, stateRepo := range ws.State.Repos {
		if _, ok := repoPaths[identity]; !ok {
			lines = append(lines, statusLine{glyphConflict, stateRepo.Path, "", "stale: no longer defined"})
		}
	}
	filePaths := fileIdentityToPath(&ws.Model)
	for identity, stateFile := range ws.State.Files {
		if _, ok := filePaths[identity]; !ok {
			lines = append(lines, statusLine{glyphConflict, stateFile.Path, "", "stale: no longer defined"})
		}
	}
	return lines
}

func printStatusLines(out io.Writer, lines []statusLine) {
	maxPath, maxBranch := 0, 0
	for _, line := range lines {
		if w := utf8.RuneCountInString(line.path); w > maxPath {
			maxPath = w
		}
		if w := utf8.RuneCountInString(line.branch); w > maxBranch {
			maxBranch = w
		}
	}
	for _, line := range lines {
		text := fmt.Sprintf("%s %-*s", line.glyph, maxPath, line.path)
		if maxBranch > 0 {
			text += fmt.Sprintf("  %-*s", maxBranch, line.branch)
		}
		if line.note != "" {
			text += "  " + line.note
		}
		fmt.Fprintln(out, strings.TrimRight(text, " "))
	}
}
