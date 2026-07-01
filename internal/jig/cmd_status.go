package jig

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type StatusOptions struct {
	Path            string
	IncludeArchived bool
}

func Status(options StatusOptions, out io.Writer) error {
	ws, err := loadWorkspace(false)
	if err != nil {
		return err
	}
	query := NodeQuery{Path: options.Path, IncludeArchived: options.IncludeArchived}
	selection, err := ws.Select(query)
	if err != nil {
		return err
	}

	installedNodes := ws.installedNodes()
	activeFiles := activeFilesForRepoSet(&ws.Model, map[string]bool{}, installedNodes.Repos, installedNodes.Files, options.IncludeArchived)

	var installed []string
	var missing []string
	var moved []string
	var remoteChanged []string
	var dirty []string
	var written []string
	var modified []string
	var conflicts []string

	for _, entry := range selection.ofKind(EntryRepo) {
		repoPath := entry.Path
		expectedAbs := filepath.Join(ws.Root, entry.Path)
		stateRepo, hasState := ws.State.Repos[entry.Identity]
		if hasState && stateRepo.Path != entry.Path && isGitRepo(filepath.Join(ws.Root, stateRepo.Path)) {
			moved = append(moved, fmt.Sprintf("%s: %s -> %s", repoPath, stateRepo.Path, entry.Path))
			if isDirty(filepath.Join(ws.Root, stateRepo.Path)) {
				dirty = append(dirty, repoPath)
			}
			continue
		}
		if !pathExists(expectedAbs) {
			missing = append(missing, repoPath)
			continue
		}
		if !isGitRepo(expectedAbs) {
			conflicts = append(conflicts, fmt.Sprintf("%s: expected path is not a Git repository", repoPath))
			continue
		}
		origin, err := gitOrigin(expectedAbs)
		if err != nil || origin != entry.Repo.Git {
			remoteChanged = append(remoteChanged, fmt.Sprintf("%s: %s -> %s", repoPath, origin, entry.Repo.Git))
		}
		if isDirty(expectedAbs) {
			dirty = append(dirty, repoPath)
		}
		installed = append(installed, repoPath)
	}

	for _, entry := range selection.ofKind(EntryFile) {
		filePath := entry.Path
		if !activeFiles[filePath] {
			continue
		}
		stateFile, hasState := ws.State.Files[entry.Identity]
		expectedAbs := filepath.Join(ws.Root, entry.Path)
		if hasState && stateFile.Path != entry.Path && pathExists(filepath.Join(ws.Root, stateFile.Path)) {
			moved = append(moved, fmt.Sprintf("%s: %s -> %s", filePath, stateFile.Path, entry.Path))
			continue
		}
		if !pathExists(expectedAbs) {
			missing = append(missing, filePath)
			continue
		}
		if !hasState {
			conflicts = append(conflicts, fmt.Sprintf("%s: existing file is not tracked", filePath))
			continue
		}
		if entry.File.Link != "" {
			info, err := os.Lstat(expectedAbs)
			if err != nil {
				conflicts = append(conflicts, fmt.Sprintf("%s: %s", filePath, err))
				continue
			}
			if info.Mode()&os.ModeSymlink == 0 {
				conflicts = append(conflicts, fmt.Sprintf("%s: expected symlink path is not a symlink", filePath))
				continue
			}
			targetEntry, _ := ws.Model.entry(entry.File.Link, EntryFile)
			expectedTarget, err := relativeSymlinkTarget(entry.Path, targetEntry.Path)
			if err != nil {
				conflicts = append(conflicts, fmt.Sprintf("%s: %s", filePath, err))
				continue
			}
			currentTarget, err := os.Readlink(expectedAbs)
			if err != nil {
				conflicts = append(conflicts, fmt.Sprintf("%s: %s", filePath, err))
				continue
			}
			if currentTarget != expectedTarget {
				modified = append(modified, filePath)
			} else {
				written = append(written, filePath)
			}
			continue
		}
		currentHash, err := fileSHA256(expectedAbs)
		if err != nil {
			conflicts = append(conflicts, fmt.Sprintf("%s: %s", filePath, err))
			continue
		}
		if currentHash != stateFile.SHA256 {
			modified = append(modified, filePath)
		} else {
			written = append(written, filePath)
		}
	}

	var stale []string
	if selection.Path == "" {
		for identity, stateRepo := range ws.State.Repos {
			if _, ok := repoIdentityToPath(&ws.Model)[identity]; !ok {
				stale = append(stale, fmt.Sprintf("%s at %s is no longer defined", identity, stateRepo.Path))
			}
		}
		for identity, stateFile := range ws.State.Files {
			if _, ok := fileIdentityToPath(&ws.Model)[identity]; !ok {
				stale = append(stale, fmt.Sprintf("%s at %s is no longer defined", identity, stateFile.Path))
			}
		}
	}

	printGroup(out, "installed", installed)
	printGroup(out, "written", written)
	printGroup(out, "moved", moved)
	printGroup(out, "missing", missing)
	printGroup(out, "remote-changed", remoteChanged)
	printGroup(out, "dirty", dirty)
	printGroup(out, "modified", modified)
	printGroup(out, "conflicts", conflicts)
	printGroup(out, "stale", stale)
	return nil
}
