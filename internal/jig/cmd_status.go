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
	All             bool // also list defined repos that are not installed
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
	activeDirs := activeDirsForRepoSet(&ws.Model, map[string]bool{}, installedNodes.Repos, installedNodes.Dirs, options.IncludeArchived)

	type result struct {
		line         statusLine
		ok           bool
		notInstalled bool
	}
	results := make([]result, len(selection.Entries))
	forEachParallel(len(selection.Entries), func(i int) {
		entry := selection.Entries[i]
		switch entry.Kind {
		case EntryRepo:
			// Status reports the workspace, not the catalog: repos that were
			// never installed are only counted unless --all is given.
			if !options.All && !repoInstalledOrTracked(ws, entry) {
				results[i] = result{notInstalled: true}
				return
			}
			results[i] = result{line: markArchived(repoStatusLine(ws, entry), entry), ok: true}
		case EntryFile:
			line, ok := fileStatusLine(ws, entry, activeFiles)
			results[i] = result{line: markArchived(line, entry), ok: ok}
		case EntryDir:
			line, ok := dirStatusLine(ws, entry, activeDirs)
			results[i] = result{line: markArchived(line, entry), ok: ok}
		}
	})
	var lines []statusLine
	notInstalled := 0
	for _, r := range results {
		if r.notInstalled {
			notInstalled++
		}
		if r.ok {
			lines = append(lines, r.line)
		}
	}

	if selection.Path == "" && len(options.Tags) == 0 {
		lines = append(lines, staleStatusLines(ws)...)
	}

	printStatusLines(out, lines, notInstalled)
	return nil
}

// repoInstalledOrTracked reports whether the repo is part of the workspace:
// checked out on disk, or tracked in state (possibly with its directory
// deleted, which sync would restore).
func repoInstalledOrTracked(ws *Workspace, entry Entry) bool {
	if _, tracked := ws.State.Repos[entry.Identity]; tracked {
		return true
	}
	return isGitRepo(filepath.Join(ws.Root, entry.Path))
}

// markArchived appends an archived note so installed archived entries are
// visible as removal candidates.
func markArchived(line statusLine, entry Entry) statusLine {
	if !entry.archived() {
		return line
	}
	if line.note == "" {
		line.note = "archived"
	} else {
		line.note += ", archived"
	}
	return line
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

// dirStatusLine summarizes a materialized subtree: per-file divergence from
// the manifest is aggregated into the note.
func dirStatusLine(ws *Workspace, entry Entry, activeDirs map[string]bool) (statusLine, bool) {
	dirPath := entry.Path
	if !activeDirs[dirPath] {
		return statusLine{}, false
	}
	expectedAbs := filepath.Join(ws.Root, entry.Path)
	stateDir, hasState := ws.State.Dirs[entry.Identity]

	if hasState && stateDir.Path != entry.Path && pathExists(filepath.Join(ws.Root, stateDir.Path)) {
		return statusLine{glyphMoved, dirPath, "", "moved from " + stateDir.Path}, true
	}
	if !pathExists(expectedAbs) {
		return statusLine{glyphMissing, dirPath, "", "missing"}, true
	}
	if !hasState {
		return statusLine{glyphConflict, dirPath, "", "untracked"}, true
	}
	modified := 0
	missing := 0
	for rel, recorded := range stateDir.Files {
		hash, err := fileSHA256(filepath.Join(expectedAbs, filepath.FromSlash(rel)))
		switch {
		case err != nil:
			missing++
		case hash != recorded:
			modified++
		}
	}
	var notes []string
	if modified > 0 {
		notes = append(notes, fmt.Sprintf("%d modified", modified))
	}
	if missing > 0 {
		notes = append(notes, fmt.Sprintf("%d missing", missing))
	}
	glyph := glyphClean
	if len(notes) > 0 {
		glyph = glyphDirty
	}
	return statusLine{glyph, dirPath, "", strings.Join(notes, ", ")}, true
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
	dirPaths := identityToPath(&ws.Model, EntryDir)
	for identity, stateDir := range ws.State.Dirs {
		if _, ok := dirPaths[identity]; !ok {
			lines = append(lines, statusLine{glyphConflict, stateDir.Path, "", "stale: no longer defined"})
		}
	}
	return lines
}

func printStatusLines(out io.Writer, lines []statusLine, notInstalled int) {
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
	printStatusSummary(out, lines, notInstalled)
}

var summaryGlyphLabels = []struct {
	glyph string
	label string
}{
	{glyphClean, "ok"},
	{glyphDirty, "dirty"},
	{glyphAhead, "ahead"},
	{glyphBehind, "behind"},
	{glyphDiverged, "diverged"},
	{glyphMissing, "missing"},
	{glyphMoved, "moved"},
	{glyphRemote, "remote-changed"},
	{glyphConflict, "conflict"},
}

func printStatusSummary(out io.Writer, lines []statusLine, notInstalled int) {
	if len(lines) == 0 && notInstalled == 0 {
		return
	}
	counts := map[string]int{}
	archived := 0
	for _, line := range lines {
		counts[line.glyph]++
		if line.note == "archived" || strings.HasSuffix(line.note, ", archived") {
			archived++
		}
	}
	var parts []string
	for _, entry := range summaryGlyphLabels {
		if counts[entry.glyph] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", counts[entry.glyph], entry.label))
		}
	}
	summary := fmt.Sprintf("%d entries", len(lines))
	if len(parts) > 0 {
		summary += ": " + strings.Join(parts, ", ")
	}
	var extras []string
	if archived > 0 {
		extras = append(extras, fmt.Sprintf("%d archived", archived))
	}
	if notInstalled > 0 {
		extras = append(extras, fmt.Sprintf("%d not installed", notInstalled))
	}
	if len(extras) > 0 {
		summary += " (" + strings.Join(extras, ", ") + ")"
	}
	fmt.Fprintln(out, summary)
}
