package jig

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ensureFile materializes a $file entry: the concatenation of its sources,
// written once and then updated only while the file is untouched. State
// keeps the content hash and the source blob ids, so an unchanged upstream
// costs no transfer and a local edit is never overwritten.
func (m *materializer) ensureFile(filePath string) error {
	entry, _ := m.model.entry(filePath, EntryFile)
	file := entry.File
	if file.Link != nil {
		return m.ensureLink(entry)
	}
	if file.managed != nil {
		return m.ensureManaged(entry)
	}
	srcs, err := effectiveSources(m.model, entry)
	if err != nil {
		return err
	}
	executable := file.Executable
	if file.Copy != nil {
		// The bit follows the copy target.
		target, _ := m.model.entry(file.copyPath, EntryFile)
		executable = target.File.Executable
	}
	expectedAbs := m.abs(entry.Path)

	record, hasState := m.state.Files[entry.Identity]
	if hasState {
		if hasState, err = m.relocate(entry, record.Path); err != nil {
			return err
		}
		if hasState {
			record.Path = entry.Path
			m.state.Files[entry.Identity] = record
		}
	}
	if m.allGatedOff(srcs) {
		return m.ensureWithoutSources(entry, hasState)
	}
	if cleared, err := m.clearOwnedLink(entry, hasState && record.Link != ""); err != nil {
		return err
	} else if cleared {
		record, hasState = StateFile{}, false
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
			return fmt.Errorf("expected file path is a directory: %s", entry.Path)
		}
		if !hasState {
			return errors.New("existing file is not tracked")
		}
		currentHash, err = fileSHA256(expectedAbs)
		if err != nil {
			return err
		}
		if currentHash != record.SHA256 {
			return errors.New("locally modified")
		}
		fileInfo = info
	} else if hasState {
		delete(m.state.Files, entry.Identity)
	}

	// A local file source's content hash stands in for a blob id; a git
	// source fetched outside the cache has no blob id at all, which just
	// disables the blob shortcut below.
	var parts [][]byte
	var blobs []string
	blobsKnown := true
	resolved, err := m.resolveSources(srcs, func(source SrcEntry) (string, error) {
		if source.Repo != nil {
			abs, key, _ := m.checkoutSource(source)
			part, err := os.ReadFile(abs)
			if err != nil {
				return "", err
			}
			if source.export {
				part = append(exportHeader(m.model, source), part...)
			}
			parts = append(parts, part)
			blobs = append(blobs, sha256Hex(part))
			return key, nil
		}
		if source.File != "" {
			localPath, err := expandLocalSource(source.File)
			if err != nil {
				return "", definitionError{err}
			}
			part, err := os.ReadFile(localPath)
			if err != nil {
				return "", err
			}
			parts = append(parts, part)
			blobs = append(blobs, sha256Hex(part))
			return "file:" + source.File, nil
		}
		if _, err := parseFileSrc(source.Src); err != nil {
			return "", definitionError{err}
		}
		part, blob, err := m.fetcher.content(source.Src)
		if err != nil {
			return "", err
		}
		parts = append(parts, part)
		if blob == "" {
			blobsKnown = false
		}
		blobs = append(blobs, blob)
		return source.Src, nil
	})
	if err != nil {
		return err
	}
	if resolved.none() {
		return m.ensureWithoutSources(entry, hasState)
	}
	// A root context file for a harness without per-file rules carries
	// the instructions after its own sources. Their ids join the blob
	// list, so a changed rule rewrites the file like a changed section.
	if file.rules != nil && len(resolved.keys) > 0 {
		rules, ids, rulesResolved, err := m.readRuleSources(file.rules)
		if err != nil {
			return err
		}
		if fold := renderFold(rules); fold != nil {
			parts = append(parts, fold)
		}
		blobs = append(blobs, ids...)
		resolved.keys = append(resolved.keys, rulesResolved.keys...)
		resolved.unavailable = append(resolved.unavailable, rulesResolved.unavailable...)
	}
	note := resolved.note()

	// With no source resolvable there is nothing to generate: the
	// already-written untouched file is left as is, a missing one is an
	// error.
	if len(resolved.keys) == 0 {
		if exists {
			fmt.Fprintf(m.out, "present-file: %s%s\n", filePath, note)
			return nil
		}
		return resolved.unavailableError()
	}

	if exists {
		// While any source is unavailable the written file is left as is:
		// regenerating without the missing part would drop that source's
		// content. The full rewrite happens once every source resolves.
		// Likewise when the source blobs have not moved: the tracked content
		// is unmodified, so there is nothing to transfer.
		unchanged := record.Src == resolved.srcKey() && record.SrcBlob != "" && blobsKnown && strings.Join(blobs, "+") == record.SrcBlob
		if len(resolved.unavailable) > 0 || unchanged {
			if err := ensureFileMode(expectedAbs, fileInfo, executable); err != nil {
				return err
			}
			fmt.Fprintf(m.out, "present-file: %s%s\n", filePath, note)
			return nil
		}
	}

	content := concatParts(parts)
	blob := ""
	if blobsKnown {
		blob = strings.Join(blobs, "+")
	}
	newHash := sha256Hex(content)
	m.state.Files[entry.Identity] = StateFile{Path: entry.Path, Src: resolved.srcKey(), SHA256: newHash, SrcBlob: blob}

	if exists && newHash == currentHash {
		// The content is already current; only the recorded blob moved.
		if err := ensureFileMode(expectedAbs, fileInfo, executable); err != nil {
			return err
		}
		fmt.Fprintf(m.out, "present-file: %s\n", filePath)
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(expectedAbs), 0o755); err != nil {
		return err
	}
	// WriteFile applies the mode only to a file it creates; an existing
	// file keeps its permissions, so the chmod sets them either way.
	if err := os.WriteFile(expectedAbs, content, fileMode(executable)); err != nil {
		return err
	}
	if err := os.Chmod(expectedAbs, fileMode(executable)); err != nil {
		return err
	}
	if exists {
		fmt.Fprintf(m.out, "updated-file: %s%s\n", filePath, note)
	} else {
		fmt.Fprintf(m.out, "wrote-file: %s%s\n", filePath, note)
	}
	return nil
}

// concatParts joins source parts in order with one blank line between two
// parts, so text sections never run together and a heading that opens a
// section is a heading in the result.
func concatParts(parts [][]byte) []byte {
	var content []byte
	for _, part := range parts {
		if len(content) > 0 {
			if content[len(content)-1] != '\n' {
				content = append(content, '\n')
			}
			if len(content) < 2 || content[len(content)-2] != '\n' {
				content = append(content, '\n')
			}
		}
		content = append(content, part...)
	}
	return content
}

// fileMode is the permission jig gives a written file: the schema's
// executable flag decides, in both directions.
func fileMode(executable bool) os.FileMode {
	if executable {
		return 0o755
	}
	return 0o644
}

// ensureFileMode makes the mode of an otherwise up-to-date file follow the
// schema, so flipping executable on or off takes effect without a rewrite.
func ensureFileMode(path string, info os.FileInfo, executable bool) error {
	if info.Mode().Perm() != fileMode(executable) {
		return os.Chmod(path, fileMode(executable))
	}
	return nil
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
