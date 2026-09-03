package jig

import (
	"archive/tar"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// ensureDir materializes a $dir entry: a whole subtree of a source
// repository, or the merge of several. State keeps the source tree ids plus
// a manifest of every file written, so updates overwrite only untouched
// files, deletions remove only untouched files, and user files inside the
// directory are never touched.
func (m *materializer) ensureDir(dirPath string) error {
	entry, _ := m.model.entry(dirPath, EntryDir)
	dir := entry.Dir
	if dir.Link != nil {
		return m.ensureLink(entry)
	}
	srcs, err := effectiveSources(m.model, entry)
	if err != nil {
		return err
	}
	expectedAbs := m.abs(entry.Path)

	record, hasState := m.state.Dirs[entry.Identity]
	if hasState {
		if hasState, err = m.relocate(entry, record.Path); err != nil {
			return err
		}
		if hasState {
			record.Path = entry.Path
			m.state.Dirs[entry.Identity] = record
		}
	}
	if m.allGatedOff(srcs) {
		return m.ensureWithoutSources(entry, hasState)
	}
	if cleared, err := m.clearOwnedLink(entry, hasState && record.Link != ""); err != nil {
		return err
	} else if cleared {
		record, hasState = StateDir{}, false
	}

	// A local directory source's content digest stands in for a git tree
	// id.
	type resolvedSource struct {
		mirror string
		tree   string
		local  string // local directory source; mirror is empty
	}
	var sources []resolvedSource
	var treeOIDs []string
	resolved, err := m.resolveSources(srcs, func(source SrcEntry) (string, error) {
		if source.Dir != "" {
			localAbs, err := expandLocalSource(source.Dir)
			if err != nil {
				return "", definitionError{err}
			}
			digest, err := localTreeDigest(localAbs)
			if err != nil {
				return "", err
			}
			sources = append(sources, resolvedSource{local: localAbs, tree: digest})
			treeOIDs = append(treeOIDs, digest)
			return "dir:" + source.Dir, nil
		}
		parsed, err := parseDirSrc(source.Src)
		if err != nil {
			return "", definitionError{err}
		}
		mirror, err := m.fetcher.mirror(parsed.GitURL)
		if err != nil {
			return "", err
		}
		srcPath, err := resolveSrcPath(mirror, parsed)
		if err != nil {
			return "", definitionError{err}
		}
		treeRef := "HEAD^{tree}"
		if srcPath != "" {
			treeRef = "HEAD:" + srcPath
		}
		treeOut, err := git(mirror, "rev-parse", treeRef)
		if err != nil {
			return "", fmt.Errorf("subtree not found: %s", shortError(err))
		}
		treeOID := strings.TrimSpace(treeOut)
		if objType, err := git(mirror, "cat-file", "-t", treeOID); err != nil || strings.TrimSpace(objType) != "tree" {
			return "", fmt.Errorf("%s is not a directory in the source repository", srcPath)
		}
		sources = append(sources, resolvedSource{mirror: mirror, tree: treeOID})
		treeOIDs = append(treeOIDs, treeOID)
		return source.Src, nil
	})
	if err != nil {
		return err
	}
	if resolved.none() {
		return m.ensureWithoutSources(entry, hasState)
	}
	srcKey := resolved.srcKey()
	combinedTree := strings.Join(treeOIDs, "+")
	note := resolved.note()

	// With no source resolvable there is nothing to materialize: an
	// already-written directory is left as is, a missing one is an error.
	if len(sources) == 0 {
		if hasState && pathExists(expectedAbs) {
			fmt.Fprintf(m.out, "present-dir: %s%s\n", dirPath, note)
			return nil
		}
		return resolved.unavailableError()
	}

	if hasState && record.Src == srcKey && record.Tree == combinedTree && manifestClean(expectedAbs, record.Files) {
		fmt.Fprintf(m.out, "present-dir: %s%s\n", dirPath, note)
		return nil
	}

	oldManifest := map[string]string{}
	if hasState {
		oldManifest = record.Files
	}
	newManifest := map[string]string{}
	var counts dirCounts
	// Later sources override earlier ones, so a source list reads as base
	// layers first and overrides after. Materializing in reverse order makes
	// the first-claim rule award a conflict to the last listed source.
	for i := len(sources) - 1; i >= 0; i-- {
		var err error
		if sources[i].local != "" {
			err = materializeLocalTree(sources[i].local, expectedAbs, oldManifest, newManifest, &counts)
		} else {
			err = materializeTree(sources[i].mirror, sources[i].tree, expectedAbs, oldManifest, newManifest, &counts)
		}
		if err != nil {
			return err
		}
	}

	// Files that disappeared upstream: delete only untouched ones. While
	// any source is unavailable its files cannot be told apart from truly
	// deleted ones, so nothing is deleted and vanished entries stay
	// tracked until every source resolves again.
	for rel, oldHash := range oldManifest {
		if _, stillThere := newManifest[rel]; stillThere {
			continue
		}
		if len(resolved.unavailable) > 0 {
			newManifest[rel] = oldHash
			continue
		}
		target := filepath.Join(expectedAbs, filepath.FromSlash(rel))
		if !pathEntryExists(target) {
			continue
		}
		if isSymlink(target) {
			counts.abandoned++
			continue
		}
		localHash, err := fileSHA256(target)
		if err == nil && localHash == oldHash {
			if err := os.Remove(target); err != nil {
				return err
			}
			pruneEmptyParents(m.root, filepath.Dir(filepath.Join(entry.Path, filepath.FromSlash(rel))))
			counts.deleted++
		} else {
			counts.abandoned++
		}
	}

	m.state.Dirs[entry.Identity] = StateDir{Path: entry.Path, Src: srcKey, Tree: combinedTree, Files: newManifest}
	fmt.Fprintln(m.out, dirMessage(dirPath, hasState, counts)+note)
	return nil
}

type dirCounts struct {
	added     int
	updated   int
	unchanged int
	kept      int // locally modified files that were not overwritten
	deleted   int // removed because they vanished upstream and were untouched
	abandoned int // vanished upstream but locally modified; left as untracked
	shadowed  int // provided by more than one source; the last source won
}

func dirMessage(dirPath string, hadState bool, counts dirCounts) string {
	if counts.added+counts.updated+counts.kept+counts.deleted+counts.abandoned+counts.shadowed == 0 {
		return "present-dir: " + dirPath
	}
	verb := "wrote-dir"
	if hadState {
		verb = "updated-dir"
	}
	var parts []string
	add := func(n int, label string) {
		if n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, label))
		}
	}
	add(counts.added, "added")
	add(counts.updated, "updated")
	add(counts.deleted, "deleted")
	add(counts.kept, "modified kept")
	add(counts.abandoned, "left untracked")
	add(counts.shadowed, "shadowed")
	return fmt.Sprintf("%s: %s (%s)", verb, dirPath, strings.Join(parts, ", "))
}

// manifestClean reports whether every manifest file exists locally with the
// recorded content.
func manifestClean(dirAbs string, manifest map[string]string) bool {
	for rel, recorded := range manifest {
		path := filepath.Join(dirAbs, filepath.FromSlash(rel))
		if isSymlink(path) {
			return false
		}
		hash, err := fileSHA256(path)
		if err != nil || hash != recorded {
			return false
		}
	}
	return true
}

// materializeTree streams `git archive` of the tree from the mirror into
// dirAbs, merging into manifest. Files already claimed by a source
// materialized before this one are shadowed (sources apply in reverse list
// order, so the last listed source claims a conflicted path first); files
// matching the old manifest (untouched) are overwritten; locally modified
// files are kept and counted.
func materializeTree(mirror string, treeOID string, dirAbs string, oldManifest map[string]string, manifest map[string]string, counts *dirCounts) error {
	cmd := exec.Command("git", "archive", treeOID)
	cmd.Dir = mirror
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	if err := mergeArchive(stdout, dirAbs, oldManifest, manifest, counts); err != nil {
		// Stopping mid-stream leaves git blocked on a pipe nobody reads;
		// kill it so Wait returns instead of hanging.
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return err
	}
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("git archive: %s", err)
	}
	return nil
}

// mergeArchive merges every regular file of a tar stream into dirAbs.
func mergeArchive(stream io.Reader, dirAbs string, oldManifest map[string]string, manifest map[string]string, counts *dirCounts) error {
	reader := tar.NewReader(stream)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		rel := filepath.ToSlash(filepath.Clean(header.Name))
		content, err := io.ReadAll(reader)
		if err != nil {
			return err
		}
		if err := mergeFileIntoDir(rel, content, header.FileInfo().Mode().Perm(), dirAbs, oldManifest, manifest, counts); err != nil {
			return err
		}
	}
}

// mergeFileIntoDir applies one source file to the merged directory under the
// manifest rules: paths already claimed by a source materialized earlier are
// shadowed, untouched files are overwritten, locally modified files kept.
func mergeFileIntoDir(rel string, content []byte, mode os.FileMode, dirAbs string, oldManifest map[string]string, manifest map[string]string, counts *dirCounts) error {
	if err := validateSafePath(rel); err != nil {
		return fmt.Errorf("unsafe path in source tree: %q", rel)
	}
	if _, claimed := manifest[rel]; claimed {
		counts.shadowed++
		return nil
	}
	newHash := sha256Hex(content)
	manifest[rel] = newHash

	target := filepath.Join(dirAbs, filepath.FromSlash(rel))
	if pathEntryExists(target) {
		// A symlink here cannot be a file this $dir wrote (manifests track
		// regular files only, and hashing through the link may fail on
		// loops); keep it like a locally modified file.
		if isSymlink(target) {
			counts.kept++
			return nil
		}
		localHash, err := fileSHA256(target)
		if err != nil {
			return err
		}
		if localHash == newHash {
			counts.unchanged++
			return nil
		}
		oldHash, tracked := oldManifest[rel]
		if !tracked || localHash != oldHash {
			counts.kept++
			return nil
		}
		if err := os.WriteFile(target, content, mode); err != nil {
			return err
		}
		_ = os.Chmod(target, mode)
		counts.updated++
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(target, content, mode); err != nil {
		return err
	}
	counts.added++
	return nil
}

// materializeLocalTree merges a local directory's regular files into dirAbs
// under the same manifest rules as a git tree source.
func materializeLocalTree(localAbs string, dirAbs string, oldManifest map[string]string, manifest map[string]string, counts *dirCounts) error {
	return filepath.WalkDir(localAbs, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(localAbs, path)
		if err != nil {
			return err
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return mergeFileIntoDir(filepath.ToSlash(rel), content, info.Mode().Perm(), dirAbs, oldManifest, manifest, counts)
	})
}

// localTreeDigest fingerprints a local directory source: a hash over every
// regular file's path and content hash, the local analogue of a git tree id.
func localTreeDigest(localAbs string) (string, error) {
	info, err := os.Stat(localAbs)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("not a directory: %s", localAbs)
	}
	var lines []string
	err = filepath.WalkDir(localAbs, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(localAbs, path)
		if err != nil {
			return err
		}
		hash, err := fileSHA256(path)
		if err != nil {
			return err
		}
		lines = append(lines, filepath.ToSlash(rel)+":"+hash)
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(lines)
	return sha256Hex([]byte(strings.Join(lines, "\n"))), nil
}
