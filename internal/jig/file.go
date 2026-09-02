package jig

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func ensureFile(out io.Writer, root string, model *Model, state *State, filePath string, allowMove bool, fetcher *fileFetcher, activeRepos map[string]bool, installedRepos map[string]bool) error {
	entry, _ := model.entry(filePath, EntryFile)
	file := entry.File
	if file.Link != nil {
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

	// A per-source onlyWhen gates just this source's content in the
	// concatenation.
	var activeSrcs []string
	for _, source := range file.Src {
		if source.OnlyWhen != nil && !conditionMatches(*source.OnlyWhen, activeRepos, installedRepos, model) {
			continue
		}
		activeSrcs = append(activeSrcs, source.Src)
	}
	if len(activeSrcs) == 0 {
		return ensureFileWithoutSources(out, root, state, entry, filePath, stateFile, hasState)
	}

	// A symlink at the path is not a file jig wrote (link files are handled
	// above), and pathExists would miss a dangling or looping one.
	if isSymlink(expectedAbs) {
		return fmt.Errorf("existing path is a symlink: %s", expectedRel)
	}
	exists := pathExists(expectedAbs)
	currentHash := ""
	var fileInfo os.FileInfo
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
		fileInfo = info
	} else if hasState {
		delete(state.Files, entry.Identity)
	}

	// Resolve every active source before touching the file. A source that
	// fails to resolve (unreachable repository, file missing upstream) is
	// excluded from this run and reported, so one broken source does not
	// block the whole file. A malformed source spec stays fatal: it is a
	// definition bug, not an availability problem.
	var available []string
	var parts [][]byte
	var blobs []string
	blobsKnown := true
	var unavailable []string
	for _, src := range activeSrcs {
		if _, err := parseFileSrc(src); err != nil {
			return fmt.Errorf("source %s: %s", src, shortError(err))
		}
		part, blob, err := fetcher.content(src)
		if err != nil {
			unavailable = append(unavailable, fmt.Sprintf("source %s unavailable: %s", src, shortError(err)))
			continue
		}
		available = append(available, src)
		parts = append(parts, part)
		if blob == "" {
			blobsKnown = false
		}
		blobs = append(blobs, blob)
	}
	srcKey := strings.Join(available, " ")
	note := ""
	if len(unavailable) > 0 {
		note = " (" + strings.Join(unavailable, "; ") + ")"
	}

	// With no source resolvable there is nothing to generate: the
	// already-written untouched file is left as is, a missing one is an
	// error.
	if len(available) == 0 {
		if exists {
			fmt.Fprintf(out, "present-file: %s%s\n", filePath, note)
			return nil
		}
		return fmt.Errorf("%s", strings.Join(unavailable, "; "))
	}

	if exists {
		// While any source is unavailable the written file is left as is:
		// regenerating without the missing part would drop that source's
		// content. The full rewrite happens once every source resolves.
		if len(unavailable) > 0 {
			if err := ensureFileMode(expectedAbs, fileInfo, file.Executable); err != nil {
				return err
			}
			fmt.Fprintf(out, "present-file: %s%s\n", filePath, note)
			return nil
		}
		// The tracked content is unmodified; when the source blobs have not
		// moved either, there is nothing to transfer.
		if stateFile.Src == srcKey && stateFile.SrcBlob != "" && blobsKnown && strings.Join(blobs, "+") == stateFile.SrcBlob {
			if err := ensureFileMode(expectedAbs, fileInfo, file.Executable); err != nil {
				return err
			}
			fmt.Fprintf(out, "present-file: %s\n", filePath)
			return nil
		}
	}

	content := concatParts(parts)
	blob := ""
	if blobsKnown {
		blob = strings.Join(blobs, "+")
	}
	newHash := sha256Hex(content)
	state.Files[entry.Identity] = StateFile{Path: expectedRel, Src: srcKey, SHA256: newHash, SrcBlob: blob}

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
		fmt.Fprintf(out, "updated-file: %s%s\n", filePath, note)
	} else {
		fmt.Fprintf(out, "wrote-file: %s%s\n", filePath, note)
	}
	return nil
}

// ensureFileWithoutSources converges a file entry whose sources are all
// gated off: no file is generated, and a previously written untouched file is
// removed. A locally modified file is kept but abandoned as untracked,
// mirroring how $dir treats modified files of a deactivated source.
func ensureFileWithoutSources(out io.Writer, root string, state *State, entry Entry, filePath string, stateFile StateFile, hasState bool) error {
	if !hasState {
		fmt.Fprintf(out, "inactive-file: %s (no active sources)\n", filePath)
		return nil
	}
	expectedAbs := filepath.Join(root, entry.Path)
	if pathExists(expectedAbs) && !isSymlink(expectedAbs) {
		currentHash, err := fileSHA256(expectedAbs)
		if err != nil {
			return err
		}
		if currentHash != stateFile.SHA256 {
			delete(state.Files, entry.Identity)
			fmt.Fprintf(out, "inactive-file: %s (no active sources; modified file left untracked)\n", filePath)
			return nil
		}
		if err := os.Remove(expectedAbs); err != nil {
			return err
		}
		pruneEmptyParents(root, filepath.Dir(entry.Path))
		delete(state.Files, entry.Identity)
		fmt.Fprintf(out, "removed-file: %s (no active sources)\n", filePath)
		return nil
	}
	delete(state.Files, entry.Identity)
	fmt.Fprintf(out, "inactive-file: %s (no active sources)\n", filePath)
	return nil
}

// concatParts joins source parts in order, inserting a newline between two
// parts when the earlier one does not end with one, so text sections never
// run together.
func concatParts(parts [][]byte) []byte {
	var content []byte
	for _, part := range parts {
		if len(content) > 0 && content[len(content)-1] != '\n' {
			content = append(content, '\n')
		}
		content = append(content, part...)
	}
	return content
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
	targetEntry, ok := model.entry(file.linkPath, EntryFile)
	if !ok {
		return fmt.Errorf("link target is not defined: %s", describeRef(*file.Link))
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
			state.Files[entry.Identity] = StateFile{Path: expectedRel, Link: file.linkPath}
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
	if err := makeSymlink(expectedTarget, expectedAbs); err != nil {
		return err
	}
	state.Files[entry.Identity] = StateFile{Path: expectedRel, Link: file.linkPath}
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
