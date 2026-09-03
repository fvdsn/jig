package jig

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// The delete helpers below are the one implementation of jig's data-safety
// rules for taking a tracked entry off disk. jig rm, sync --prune, and the
// deactivation of a $file or $dir whose sources are all gated off share
// them, so what counts as "locally modified" cannot drift between commands.

// modifiedPolicy says what to do with content the user changed since jig
// wrote it.
type modifiedPolicy int

const (
	refuseModified  modifiedPolicy = iota // report an error and delete nothing (jig rm, sync --prune)
	abandonModified                       // leave the modified content in place as untracked (deactivation)
	deleteModified                        // delete regardless (jig rm --force)
)

// deleteTrackedRepo removes a repository checkout. Unless forced, a checkout
// with uncommitted changes, unpushed commits, or no upstream is refused: any
// of those would lose work.
func deleteTrackedRepo(root string, rel string, force bool) error {
	abs := filepath.Join(root, rel)
	if !isGitRepo(abs) {
		return nil
	}
	if !force {
		if isDirty(abs) {
			return errors.New("uncommitted changes")
		}
		if reason := unpushedReason(abs); reason != "" {
			return errors.New(reason)
		}
	}
	if err := os.RemoveAll(abs); err != nil {
		return err
	}
	pruneEmptyParents(root, filepath.Dir(rel))
	return nil
}

// deleteTrackedFile removes a written file or link file. A file whose
// content no longer matches the recorded hash, or that was replaced by a
// symlink, is locally modified; a link file is only ever a symlink, so
// removing it loses nothing. kept reports whether modified content was left
// behind under abandonModified.
func deleteTrackedFile(root string, rel string, stateFile StateFile, policy modifiedPolicy) (kept bool, err error) {
	abs := filepath.Join(root, rel)
	if !pathEntryExists(abs) {
		return false, nil
	}
	if stateFile.SHA256 != "" && policy != deleteModified {
		modified := isSymlink(abs)
		if !modified {
			hash, err := fileSHA256(abs)
			modified = err != nil || hash != stateFile.SHA256
		}
		if modified {
			if policy == refuseModified {
				return false, errors.New("locally modified")
			}
			return true, nil
		}
	}
	if err := os.Remove(abs); err != nil {
		return false, err
	}
	pruneEmptyParents(root, filepath.Dir(rel))
	return false, nil
}

// deleteTrackedDir removes a materialized subtree: the symlink of a link
// dir, otherwise the manifest-tracked files. Files the user added inside
// the directory are never touched; a manifest file whose content changed or
// that became a symlink is locally modified. kept counts the modified files
// left behind under abandonModified.
func deleteTrackedDir(root string, rel string, stateDir StateDir, policy modifiedPolicy) (kept int, err error) {
	abs := filepath.Join(root, rel)
	if stateDir.Link != "" {
		if isSymlink(abs) {
			if err := os.Remove(abs); err != nil {
				return 0, err
			}
			pruneEmptyParents(root, filepath.Dir(rel))
		}
		return 0, nil
	}
	if !pathExists(abs) {
		return 0, nil
	}
	var untouched []string
	for fileRel, recorded := range stateDir.Files {
		target := filepath.Join(abs, filepath.FromSlash(fileRel))
		if !pathEntryExists(target) {
			continue
		}
		modified := isSymlink(target)
		if !modified {
			hash, err := fileSHA256(target)
			modified = err != nil || hash != recorded
		}
		if modified && policy != deleteModified {
			kept++
			continue
		}
		untouched = append(untouched, fileRel)
	}
	if kept > 0 && policy == refuseModified {
		return kept, fmt.Errorf("%d locally modified files", kept)
	}
	for _, fileRel := range untouched {
		if err := os.Remove(filepath.Join(abs, filepath.FromSlash(fileRel))); err != nil {
			return kept, err
		}
		pruneEmptyParents(root, filepath.Dir(filepath.Join(rel, filepath.FromSlash(fileRel))))
	}
	_ = os.Remove(abs)
	pruneEmptyParents(root, filepath.Dir(rel))
	return kept, nil
}

// unpushedReason reports why deleting the checkout could lose commits: the
// current branch is ahead of its upstream, or has no upstream at all.
func unpushedReason(path string) string {
	ahead, _, ok := aheadBehind(path)
	if !ok {
		return "current branch has no upstream"
	}
	if ahead > 0 {
		return fmt.Sprintf("%d unpushed commits", ahead)
	}
	return ""
}
