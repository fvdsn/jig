package jig

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func validateSafePath(path string) error {
	if path == "" {
		return errors.New("path is empty")
	}
	if filepath.IsAbs(path) || strings.HasPrefix(path, "/") {
		return errors.New("path must be relative")
	}
	if strings.HasPrefix(path, "~") {
		return errors.New("path must not start with ~")
	}
	if strings.Contains(path, "\\") {
		return errors.New("path must use / separators")
	}
	segments := strings.Split(path, "/")
	for _, segment := range segments {
		if segment == "" {
			return errors.New("path must not contain empty segments")
		}
		if segment == "." || segment == ".." {
			return errors.New("path must not contain . or .. segments")
		}
	}
	return nil
}

func relativeSymlinkTarget(linkPath string, targetPath string) (string, error) {
	fromDir := filepath.Dir(linkPath)
	if fromDir == "." {
		fromDir = ""
	}
	rel, err := filepath.Rel(fromDir, targetPath)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

// moveInstalledPath relocates an installed checkout or file from oldRel to
// newRel under root, pruning parent directories left empty by the move.
// It returns the message describing the move.
func moveInstalledPath(root string, entryPath string, oldRel string, newRel string, label string) (string, error) {
	oldAbs := filepath.Join(root, oldRel)
	newAbs := filepath.Join(root, newRel)
	if pathExists(newAbs) {
		// On a case-insensitive filesystem a move that only changes letter
		// case sees itself at the target; that is a rename, not a conflict.
		// Renaming through a temporary name makes the case change stick.
		if !samePath(oldAbs, newAbs) {
			return "", fmt.Errorf("target path already exists: %s", newRel)
		}
		tmp := newAbs + ".jig-move"
		if pathEntryExists(tmp) {
			return "", fmt.Errorf("temporary move path already exists: %s", tmp)
		}
		if err := os.Rename(oldAbs, tmp); err != nil {
			return "", err
		}
		oldAbs = tmp
	}
	if err := os.MkdirAll(filepath.Dir(newAbs), 0o755); err != nil {
		return "", err
	}
	if err := os.Rename(oldAbs, newAbs); err != nil {
		return "", err
	}
	pruneEmptyParents(root, filepath.Dir(oldRel))
	return fmt.Sprintf("%s: %s: %s -> %s", label, entryPath, oldRel, newRel), nil
}

// samePath reports whether two paths name the same file or directory, as on
// a case-insensitive filesystem when they differ only by letter case.
func samePath(a string, b string) bool {
	infoA, errA := os.Stat(a)
	infoB, errB := os.Stat(b)
	return errA == nil && errB == nil && os.SameFile(infoA, infoB)
}

func pruneEmptyParents(root string, relDir string) {
	if relDir == "." || relDir == "" {
		return
	}
	for {
		if relDir == "." || relDir == "" {
			return
		}
		abs := filepath.Join(root, relDir)
		if filepath.Clean(abs) == filepath.Clean(root) {
			return
		}
		if err := os.Remove(abs); err != nil {
			return
		}
		relDir = filepath.Dir(relDir)
	}
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func pathEntryExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// isSymlink reports whether path itself is a symbolic link.
func isSymlink(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode()&os.ModeSymlink != 0
}
