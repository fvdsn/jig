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

	// A per-source onlyWhen gates just this source's content in the
	// concatenation.
	var activeSrcs []string
	for _, source := range file.Src {
		if source.OnlyWhen != nil && !conditionMatches(*source.OnlyWhen, activeRepos, installedRepos, model) {
			continue
		}
		activeSrcs = append(activeSrcs, source.Src)
	}
	srcKey := strings.Join(activeSrcs, " ")
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
		// The tracked content is unmodified; when the source blobs have not
		// moved either, there is nothing to transfer.
		if stateFile.Src == srcKey && stateFile.SrcBlob != "" {
			blob, err := combinedSrcBlob(fetcher, activeSrcs)
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

	content, blob, err := fetchConcatenated(fetcher, activeSrcs)
	if err != nil {
		return err
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
		fmt.Fprintf(out, "updated-file: %s\n", filePath)
	} else {
		fmt.Fprintf(out, "wrote-file: %s\n", filePath)
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

// combinedSrcBlob joins the source blob ids of every active source with "+":
// the multi-source analogue of a single source's blob id.
func combinedSrcBlob(fetcher *fileFetcher, srcs []string) (string, error) {
	blobs := make([]string, 0, len(srcs))
	for _, src := range srcs {
		blob, err := fetcher.srcBlob(src)
		if err != nil {
			return "", err
		}
		blobs = append(blobs, blob)
	}
	return strings.Join(blobs, "+"), nil
}

// fetchConcatenated fetches every active source and concatenates the parts
// in order, inserting a newline between parts when one is missing. The
// combined blob id is empty when any source's blob id is unknown, so the
// next run re-fetches instead of trusting a partial key.
func fetchConcatenated(fetcher *fileFetcher, srcs []string) ([]byte, string, error) {
	var content []byte
	blobs := make([]string, 0, len(srcs))
	blobsKnown := true
	for _, src := range srcs {
		part, blob, err := fetcher.content(src)
		if err != nil {
			return nil, "", err
		}
		if len(content) > 0 && content[len(content)-1] != '\n' {
			content = append(content, '\n')
		}
		content = append(content, part...)
		if blob == "" {
			blobsKnown = false
		}
		blobs = append(blobs, blob)
	}
	if !blobsKnown {
		return content, "", nil
	}
	return content, strings.Join(blobs, "+"), nil
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
