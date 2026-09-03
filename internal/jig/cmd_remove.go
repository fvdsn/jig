package jig

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
	defer ws.Close()
	installed := ws.installedNodes()

	var targets []Entry
	var failures []string
	seen := map[string]bool{}
	for _, rawPath := range options.Paths {
		resolved, err := ws.ResolvePath(rawPath)
		if err != nil {
			return err
		}
		selection, err := ws.Model.Select(NodeQuery{Path: resolved, IncludeArchived: true, Installed: installed})
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
		exact := len(matches) == 1 && matches[0].Path == selection.Path
		if !options.Recursive && !exact {
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
		return fmt.Errorf("%d entries not removed", len(failures))
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
	case EntryDir:
		_, tracked := ws.State.Dirs[entry.Identity]
		return tracked || installed.Dirs[entry.Identity]
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
	case EntryDir:
		return removeDir(out, ws, entry, force)
	default:
		return errors.New("cannot remove this entry")
	}
}

// removalPolicy maps --force onto the shared delete helpers.
func removalPolicy(force bool) modifiedPolicy {
	if force {
		return deleteModified
	}
	return refuseModified
}

// forceHint tells the user how to override a refused removal.
func forceHint(err error) error {
	return fmt.Errorf("%s (use --force)", err)
}

func removeRepo(out io.Writer, ws *Workspace, entry Entry, force bool) error {
	rel := entry.Path
	if stateRepo, ok := ws.State.Repos[entry.Identity]; ok && isGitRepo(filepath.Join(ws.Root, stateRepo.Path)) {
		rel = stateRepo.Path
	}
	if err := deleteTrackedRepo(ws.Root, rel, force); err != nil {
		if !force && !isRemovalIOError(err) {
			return forceHint(err)
		}
		return err
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
	if _, err := deleteTrackedFile(ws.Root, rel, stateFile, removalPolicy(force)); err != nil {
		if !force && !isRemovalIOError(err) {
			return forceHint(err)
		}
		return err
	}
	delete(ws.State.Files, entry.Identity)
	fmt.Fprintf(out, "removed: %s\n", entry.Path)
	return nil
}

// removeDir deletes the manifest-tracked files of a materialized subtree,
// refusing when any of them is locally modified unless forced. Files the
// user added inside the directory are never touched.
func removeDir(out io.Writer, ws *Workspace, entry Entry, force bool) error {
	rel := entry.Path
	stateDir, tracked := ws.State.Dirs[entry.Identity]
	if tracked && pathEntryExists(filepath.Join(ws.Root, stateDir.Path)) {
		rel = stateDir.Path
	}
	if _, err := deleteTrackedDir(ws.Root, rel, stateDir, removalPolicy(force)); err != nil {
		if !force && !isRemovalIOError(err) {
			return forceHint(err)
		}
		return err
	}
	delete(ws.State.Dirs, entry.Identity)
	fmt.Fprintf(out, "removed: %s\n", entry.Path)
	return nil
}

// isRemovalIOError tells a filesystem failure apart from a safety refusal:
// only the latter is answered by --force.
func isRemovalIOError(err error) bool {
	var pathErr *os.PathError
	return errors.As(err, &pathErr)
}
