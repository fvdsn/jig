package jig

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func ensureFile(out io.Writer, root string, model *Model, state *State, filePath string, allowMove bool, refresh bool, fetcher *fileFetcher) error {
	entry, _ := model.entry(filePath, EntryFile)
	file := entry.File
	if file.Link != "" {
		return ensureLinkFile(out, root, model, state, filePath, allowMove)
	}
	stateFile, hasState := state.Files[entry.Identity]
	expectedRel := entry.Path
	expectedAbs := filepath.Join(root, expectedRel)

	if hasState && stateFile.Path != expectedRel {
		oldAbs := filepath.Join(root, stateFile.Path)
		if pathExists(oldAbs) {
			currentHash, err := fileSHA256(oldAbs)
			if err != nil {
				return err
			}
			if currentHash != stateFile.SHA256 {
				return fmt.Errorf("locally modified")
			}
			if !allowMove {
				return fmt.Errorf("already written at %s; run jig sync to move it", stateFile.Path)
			}
			message, err := moveInstalledPath(root, filePath, stateFile.Path, expectedRel, "moved-file")
			if err != nil {
				return err
			}
			fmt.Fprintln(out, message)
			stateFile.Path = expectedRel
			state.Files[entry.Identity] = stateFile
			hasState = true
		} else {
			delete(state.Files, entry.Identity)
			hasState = false
		}
	}

	exists := pathExists(expectedAbs)
	currentHash := ""
	if exists {
		info, err := os.Stat(expectedAbs)
		if err != nil {
			return err
		}
		if info.IsDir() {
			return fmt.Errorf("expected file path is a directory: %s", expectedRel)
		}
		if !hasState {
			return fmt.Errorf("existing file is not tracked")
		}
		currentHash, err = fileSHA256(expectedAbs)
		if err != nil {
			return err
		}
		if currentHash != stateFile.SHA256 {
			return fmt.Errorf("locally modified")
		}
		// The tracked content is unmodified; when the source blob has not
		// moved either, there is nothing to transfer.
		if !refresh && stateFile.Src == file.Src && stateFile.SrcBlob != "" {
			blob, err := fetcher.srcBlob(file.Src)
			if err != nil {
				fmt.Fprintf(out, "present-file: %s (source not checked: %s)\n", filePath, shortError(err))
				return nil
			}
			if blob == stateFile.SrcBlob {
				if err := ensureFileMode(expectedAbs, info, file.Executable); err != nil {
					return err
				}
				fmt.Fprintf(out, "present-file: %s\n", filePath)
				return nil
			}
		}
	} else if hasState {
		delete(state.Files, entry.Identity)
	}

	content, blob, err := fetcher.content(file.Src)
	if err != nil {
		return err
	}
	newHash := sha256Hex(content)
	state.Files[entry.Identity] = StateFile{Path: expectedRel, Src: file.Src, SHA256: newHash, SrcBlob: blob}

	if exists && newHash == currentHash {
		// The content is already current; only the recorded blob moved.
		info, err := os.Stat(expectedAbs)
		if err != nil {
			return err
		}
		if err := ensureFileMode(expectedAbs, info, file.Executable); err != nil {
			return err
		}
		fmt.Fprintf(out, "present-file: %s\n", filePath)
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(expectedAbs), 0o755); err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if file.Executable {
		mode = 0o755
	}
	if err := os.WriteFile(expectedAbs, content, mode); err != nil {
		return err
	}
	if file.Executable {
		_ = os.Chmod(expectedAbs, 0o755)
	}
	if exists {
		fmt.Fprintf(out, "updated-file: %s\n", filePath)
	} else {
		fmt.Fprintf(out, "wrote-file: %s\n", filePath)
	}
	return nil
}

// ensureFileMode fixes the executable bit on an otherwise up-to-date file.
func ensureFileMode(path string, info os.FileInfo, executable bool) error {
	if executable && info.Mode().Perm() != 0o755 {
		return os.Chmod(path, 0o755)
	}
	return nil
}

func ensureLinkFile(out io.Writer, root string, model *Model, state *State, filePath string, allowMove bool) error {
	entry, _ := model.entry(filePath, EntryFile)
	file := entry.File
	targetEntry, ok := model.entry(file.Link, EntryFile)
	if !ok {
		return fmt.Errorf("link target is not defined: %s", file.Link)
	}
	if !pathExists(filepath.Join(root, targetEntry.Path)) {
		return fmt.Errorf("link target is missing: %s", targetEntry.Path)
	}

	stateFile, hasState := state.Files[entry.Identity]
	expectedRel := entry.Path
	expectedAbs := filepath.Join(root, expectedRel)
	expectedTarget, err := relativeSymlinkTarget(expectedRel, targetEntry.Path)
	if err != nil {
		return err
	}

	if hasState && stateFile.Path != expectedRel {
		oldAbs := filepath.Join(root, stateFile.Path)
		if pathExists(oldAbs) {
			if !allowMove {
				return fmt.Errorf("already written at %s; run jig sync to move it", stateFile.Path)
			}
			message, err := moveInstalledPath(root, filePath, stateFile.Path, expectedRel, "moved-file")
			if err != nil {
				return err
			}
			fmt.Fprintln(out, message)
			hasState = true
		} else {
			delete(state.Files, entry.Identity)
			hasState = false
		}
	}

	if pathExists(expectedAbs) {
		info, err := os.Lstat(expectedAbs)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink == 0 {
			return fmt.Errorf("expected symlink path exists and is not a symlink: %s", expectedRel)
		}
		currentTarget, err := os.Readlink(expectedAbs)
		if err != nil {
			return err
		}
		if currentTarget == expectedTarget {
			state.Files[entry.Identity] = StateFile{Path: expectedRel, Link: file.Link}
			fmt.Fprintf(out, "present-file: %s\n", filePath)
			return nil
		}
		if !hasState || stateFile.Link == "" {
			return fmt.Errorf("existing symlink has different target")
		}
		if err := os.Remove(expectedAbs); err != nil {
			return err
		}
	}

	if err := os.MkdirAll(filepath.Dir(expectedAbs), 0o755); err != nil {
		return err
	}
	if err := os.Symlink(expectedTarget, expectedAbs); err != nil {
		return err
	}
	state.Files[entry.Identity] = StateFile{Path: expectedRel, Link: file.Link}
	fmt.Fprintf(out, "linked-file: %s\n", filePath)
	return nil
}

func installedFileIdentitySet(root string, model *Model, state *State) map[string]bool {
	installed := map[string]bool{}
	identityToPath := fileIdentityToPath(model)
	for identity, stateFile := range state.Files {
		if _, ok := identityToPath[identity]; !ok {
			continue
		}
		if pathEntryExists(filepath.Join(root, stateFile.Path)) {
			installed[identity] = true
		}
	}
	return installed
}

func fileSHA256(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return sha256Hex(data), nil
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
