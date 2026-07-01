package jig

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func ensureFile(out io.Writer, root string, model *Model, state *State, filePath string, allowMove bool) error {
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
			if err := moveInstalledPath(out, root, filePath, stateFile.Path, expectedRel, "moved-file"); err != nil {
				return err
			}
			stateFile.Path = expectedRel
			state.Files[entry.Identity] = stateFile
			hasState = true
		} else {
			delete(state.Files, entry.Identity)
			hasState = false
		}
	}

	if pathExists(expectedAbs) {
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
		currentHash, err := fileSHA256(expectedAbs)
		if err != nil {
			return err
		}
		if currentHash != stateFile.SHA256 {
			return fmt.Errorf("locally modified")
		}
	} else if hasState {
		delete(state.Files, entry.Identity)
	}

	content, err := fetchFileContent(file.Src)
	if err != nil {
		return err
	}
	newHash := sha256Hex(content)

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
	state.Files[entry.Identity] = StateFile{Path: expectedRel, Src: file.Src, SHA256: newHash}
	fmt.Fprintf(out, "wrote-file: %s\n", filePath)
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
			if err := moveInstalledPath(out, root, filePath, stateFile.Path, expectedRel, "moved-file"); err != nil {
				return err
			}
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

func fetchFileContent(src string) ([]byte, error) {
	parsed, err := parseFileSrc(src)
	if err != nil {
		return nil, err
	}
	return fetchGitFile(parsed.GitURL, "", parsed.Path)
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
