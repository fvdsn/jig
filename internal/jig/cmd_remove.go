package jig

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type RemoveOptions struct {
	Paths     []string
	Recursive bool
	Force     bool
}

// Remove uninstalls repositories and files: it deletes their checkouts and
// drops them from state so sync stops restoring them. Mirrors rm ergonomics:
// exact entry paths remove directly, anything broader requires --recursive,
// and per-entry failures are reported while the rest proceeds.
func Remove(options RemoveOptions, out io.Writer) error {
	ws, err := loadWorkspace(true)
	if err != nil {
		return err
	}
	installed := ws.installedNodes()

	var targets []Entry
	var failures []string
	seen := map[string]bool{}
	for _, rawPath := range options.Paths {
		selection, err := ws.Model.Select(NodeQuery{Path: rawPath, IncludeArchived: true, Installed: installed})
		if err != nil {
			return err
		}
		var matches []Entry
		for _, entry := range selection.Entries {
			if entry.Kind == EntryGroup {
				continue
			}
			if !removable(ws, entry, installed) {
				continue
			}
			matches = append(matches, entry)
		}
		if len(matches) == 0 {
			failures = append(failures, fmt.Sprintf("%s: nothing installed matches", rawPath))
			continue
		}
		if !options.Recursive && !(len(matches) == 1 && matches[0].Path == selection.Path) {
			failures = append(failures, fmt.Sprintf("%s: matches %d entries; use -r to remove them all", rawPath, len(matches)))
			continue
		}
		for _, entry := range matches {
			if !seen[entry.Identity] {
				seen[entry.Identity] = true
				targets = append(targets, entry)
			}
		}
	}

	for _, entry := range targets {
		if err := removeEntry(out, ws, entry, options.Force); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %s", entry.Path, err))
		}
	}
	if err := saveState(ws.Root, ws.State); err != nil {
		return err
	}
	if len(failures) > 0 {
		printGroup(out, "not-removed", failures)
		return errors.New("some entries were not removed")
	}
	return nil
}

// removable reports whether jig owns the entry: it is installed or still
// tracked in state (covering checkouts whose directory was already deleted).
func removable(ws *Workspace, entry Entry, installed InstalledNodes) bool {
	switch entry.Kind {
	case EntryRepo:
		_, tracked := ws.State.Repos[entry.Identity]
		return tracked || installed.Repos[entry.Identity]
	case EntryFile:
		_, tracked := ws.State.Files[entry.Identity]
		return tracked || installed.Files[entry.Identity]
	default:
		return false
	}
}

func removeEntry(out io.Writer, ws *Workspace, entry Entry, force bool) error {
	switch entry.Kind {
	case EntryRepo:
		return removeRepo(out, ws, entry, force)
	case EntryFile:
		return removeFile(out, ws, entry, force)
	default:
		return errors.New("cannot remove this entry")
	}
}

func removeRepo(out io.Writer, ws *Workspace, entry Entry, force bool) error {
	rel := entry.Path
	if stateRepo, ok := ws.State.Repos[entry.Identity]; ok && isGitRepo(filepath.Join(ws.Root, stateRepo.Path)) {
		rel = stateRepo.Path
	}
	abs := filepath.Join(ws.Root, rel)
	if isGitRepo(abs) {
		if !force {
			if isDirty(abs) {
				return errors.New("uncommitted changes (use --force)")
			}
			if reason := unpushedReason(abs); reason != "" {
				return fmt.Errorf("%s (use --force)", reason)
			}
		}
		if err := os.RemoveAll(abs); err != nil {
			return err
		}
		pruneEmptyParents(ws.Root, filepath.Dir(rel))
	}
	delete(ws.State.Repos, entry.Identity)
	fmt.Fprintf(out, "removed: %s\n", entry.Path)
	return nil
}

func removeFile(out io.Writer, ws *Workspace, entry Entry, force bool) error {
	rel := entry.Path
	stateFile, tracked := ws.State.Files[entry.Identity]
	if tracked && pathEntryExists(filepath.Join(ws.Root, stateFile.Path)) {
		rel = stateFile.Path
	}
	abs := filepath.Join(ws.Root, rel)
	if pathEntryExists(abs) {
		if !force && tracked && stateFile.SHA256 != "" {
			currentHash, err := fileSHA256(abs)
			if err != nil {
				return err
			}
			if currentHash != stateFile.SHA256 {
				return errors.New("locally modified (use --force)")
			}
		}
		if err := os.Remove(abs); err != nil {
			return err
		}
		pruneEmptyParents(ws.Root, filepath.Dir(rel))
	}
	delete(ws.State.Files, entry.Identity)
	fmt.Fprintf(out, "removed: %s\n", entry.Path)
	return nil
}

// unpushedReason reports why deleting the checkout could lose commits: the
// current branch is ahead of its upstream, or has no upstream at all.
func unpushedReason(path string) string {
	out, err := git(path, "rev-list", "--count", "@{upstream}..HEAD")
	if err != nil {
		return "current branch has no upstream"
	}
	if count := strings.TrimSpace(out); count != "0" {
		return count + " unpushed commits"
	}
	return ""
}
