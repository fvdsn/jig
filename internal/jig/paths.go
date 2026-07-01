package jig

import (
	"errors"
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
