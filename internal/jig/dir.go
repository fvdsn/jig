package jig

import (
	"archive/tar"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ensureDir materializes a $dir entry: a whole subtree of a source
// repository. State keeps the source tree id plus a manifest of every file
// written, so updates overwrite only untouched files, deletions remove only
// untouched files, and user files inside the directory are never touched.
func ensureDir(out io.Writer, root string, model *Model, state *State, dirPath string, allowMove bool, fetcher *fileFetcher, activeRepos map[string]bool, installedRepos map[string]bool) error {
	entry, _ := model.entry(dirPath, EntryDir)
	dir := entry.Dir
	if dir.Link != nil {
		return ensureLinkDir(out, root, model, state, dirPath, allowMove)
	}
	// A copy entry materializes its target's sources as a real directory, so
	// the contents match the target's by construction without requiring the
	// target to be installed.
	srcs := dir.Src
	if dir.Copy != nil {
		target, ok := model.entry(dir.copyPath, EntryDir)
		if !ok {
			return fmt.Errorf("copy target is not defined: %s", describeRef(*dir.Copy))
		}
		srcs = target.Dir.Src
	}
	stateDir, hasState := state.Dirs[entry.Identity]
	expectedRel := entry.Path
	expectedAbs := filepath.Join(root, expectedRel)

	if hasState && stateDir.Path != expectedRel {
		oldAbs := filepath.Join(root, stateDir.Path)
		if pathExists(oldAbs) {
			if !allowMove {
				return fmt.Errorf("already written at %s; run jig sync to move it", stateDir.Path)
			}
			message, err := moveInstalledPath(root, dirPath, stateDir.Path, expectedRel, "moved-dir")
			if err != nil {
				return err
			}
			fmt.Fprintln(out, message)
			stateDir.Path = expectedRel
			state.Dirs[entry.Identity] = stateDir
		} else {
			delete(state.Dirs, entry.Identity)
			hasState = false
		}
	}

	// A symlink at the path is not a directory jig materialized; writing
	// through it would land in the target. The one exception is a jig-owned
	// link being converted to a copy: the recorded symlink is replaced by
	// the materialized directory.
	if isSymlink(expectedAbs) {
		if !hasState || stateDir.Link == "" {
			return fmt.Errorf("existing path is a symlink: %s", expectedRel)
		}
		if err := os.Remove(expectedAbs); err != nil {
			return err
		}
		delete(state.Dirs, entry.Identity)
		stateDir, hasState = StateDir{}, false
	}

	// Resolve every source before touching the workspace. A source that
	// fails to resolve (unreachable repository, subtree missing upstream)
	// is excluded from this run's merge and reported, so one broken
	// source does not block the whole directory. A malformed source spec
	// stays fatal: it is a definition bug, not an availability problem.
	type resolvedSource struct {
		mirror string
		tree   string
	}
	var sources []resolvedSource
	var treeOIDs []string
	var activeSrcs []string
	var unavailable []string
	for _, dirSource := range srcs {
		// A per-source onlyWhen gates just this source's tree in the merge.
		if dirSource.OnlyWhen != nil && !conditionMatches(*dirSource.OnlyWhen, activeRepos, installedRepos, model) {
			continue
		}
		parsed, err := parseDirSrc(dirSource.Src)
		if err != nil {
			return fmt.Errorf("source %s: %s", dirSource.Src, shortError(err))
		}
		mirror, err := fetcher.mirror(parsed.GitURL)
		if err != nil {
			unavailable = append(unavailable, fmt.Sprintf("source %s unavailable: %s", dirSource.Src, shortError(err)))
			continue
		}
		srcPath, err := resolveSrcPath(mirror, parsed)
		if err != nil {
			return fmt.Errorf("source %s: %s", dirSource.Src, shortError(err))
		}
		treeRef := "HEAD^{tree}"
		if srcPath != "" {
			treeRef = "HEAD:" + srcPath
		}
		treeOut, err := git(mirror, "rev-parse", treeRef)
		if err != nil {
			unavailable = append(unavailable, fmt.Sprintf("source %s unavailable: subtree not found: %s", dirSource.Src, shortError(err)))
			continue
		}
		treeOID := strings.TrimSpace(treeOut)
		if objType, err := git(mirror, "cat-file", "-t", treeOID); err != nil || strings.TrimSpace(objType) != "tree" {
			unavailable = append(unavailable, fmt.Sprintf("source %s unavailable: %s is not a directory in the source repository", dirSource.Src, srcPath))
			continue
		}
		sources = append(sources, resolvedSource{mirror, treeOID})
		treeOIDs = append(treeOIDs, treeOID)
		activeSrcs = append(activeSrcs, dirSource.Src)
	}
	srcKey := strings.Join(activeSrcs, " ")
	combinedTree := strings.Join(treeOIDs, "+")
	note := ""
	if len(unavailable) > 0 {
		note = " (" + strings.Join(unavailable, "; ") + ")"
	}

	// With no source resolvable there is nothing to materialize: an
	// already-written directory is left as is, a missing one is an error.
	if len(sources) == 0 && len(unavailable) > 0 {
		if hasState && pathExists(expectedAbs) {
			fmt.Fprintf(out, "present-dir: %s%s\n", dirPath, note)
			return nil
		}
		return fmt.Errorf("%s", strings.Join(unavailable, "; "))
	}

	if hasState && stateDir.Src == srcKey && stateDir.Tree == combinedTree && manifestClean(expectedAbs, stateDir.Files) {
		fmt.Fprintf(out, "present-dir: %s%s\n", dirPath, note)
		return nil
	}

	oldManifest := map[string]string{}
	if hasState {
		oldManifest = stateDir.Files
	}
	newManifest := map[string]string{}
	var counts dirCounts
	// Later sources override earlier ones, so a source list reads as base
	// layers first and overrides after. Materializing in reverse order makes
	// the first-claim rule award a conflict to the last listed source.
	for i := len(sources) - 1; i >= 0; i-- {
		if err := materializeTree(sources[i].mirror, sources[i].tree, expectedAbs, oldManifest, newManifest, &counts); err != nil {
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
		if len(unavailable) > 0 {
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
			pruneEmptyParents(root, filepath.Dir(filepath.Join(expectedRel, filepath.FromSlash(rel))))
			counts.deleted++
		} else {
			counts.abandoned++
		}
	}

	state.Dirs[entry.Identity] = StateDir{Path: expectedRel, Src: srcKey, Tree: combinedTree, Files: newManifest}
	fmt.Fprintln(out, dirMessage(dirPath, hasState, counts)+note)
	return nil
}

// ensureLinkDir creates a relative symlink to another $dir entry, mirroring
// link files.
func ensureLinkDir(out io.Writer, root string, model *Model, state *State, dirPath string, allowMove bool) error {
	entry, _ := model.entry(dirPath, EntryDir)
	dir := entry.Dir
	targetEntry, ok := model.entry(dir.linkPath, EntryDir)
	if !ok {
		return fmt.Errorf("link target is not defined: %s", describeRef(*dir.Link))
	}
	if !pathExists(filepath.Join(root, targetEntry.Path)) {
		return fmt.Errorf("link target is missing: %s", targetEntry.Path)
	}

	stateDir, hasState := state.Dirs[entry.Identity]
	expectedRel := entry.Path
	expectedAbs := filepath.Join(root, expectedRel)
	expectedTarget, err := relativeSymlinkTarget(expectedRel, targetEntry.Path)
	if err != nil {
		return err
	}

	if hasState && stateDir.Path != expectedRel {
		oldAbs := filepath.Join(root, stateDir.Path)
		if pathEntryExists(oldAbs) {
			if !allowMove {
				return fmt.Errorf("already written at %s; run jig sync to move it", stateDir.Path)
			}
			message, err := moveInstalledPath(root, dirPath, stateDir.Path, expectedRel, "moved-dir")
			if err != nil {
				return err
			}
			fmt.Fprintln(out, message)
		} else {
			delete(state.Dirs, entry.Identity)
			hasState = false
		}
	}

	if pathEntryExists(expectedAbs) {
		info, err := os.Lstat(expectedAbs)
		if err != nil {
			return err
		}
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			currentTarget, err := os.Readlink(expectedAbs)
			if err != nil {
				return err
			}
			if currentTarget == expectedTarget {
				state.Dirs[entry.Identity] = StateDir{Path: expectedRel, Link: dir.linkPath}
				fmt.Fprintf(out, "present-dir: %s\n", dirPath)
				return nil
			}
			if !hasState || stateDir.Link == "" {
				return fmt.Errorf("existing symlink has different target")
			}
			if err := os.Remove(expectedAbs); err != nil {
				return err
			}
		case hasState && stateDir.Link == "" && len(stateDir.Files) > 0:
			// Converting a copy (or src) entry to a link: a fully untouched
			// materialization is replaced by the symlink; modified or
			// untracked files block the conversion.
			if !manifestClean(expectedAbs, stateDir.Files) {
				return fmt.Errorf("locally modified files; refusing to replace with a symlink: %s", expectedRel)
			}
			for rel := range stateDir.Files {
				if err := os.Remove(filepath.Join(expectedAbs, filepath.FromSlash(rel))); err != nil {
					return err
				}
			}
			removeEmptyDirTree(expectedAbs)
			if pathEntryExists(expectedAbs) {
				return fmt.Errorf("directory was not fully cleared (untracked files remain, or a removal failed); refusing to replace with a symlink: %s", expectedRel)
			}
		default:
			return fmt.Errorf("expected symlink path exists and is not a symlink: %s", expectedRel)
		}
	}

	if err := os.MkdirAll(filepath.Dir(expectedAbs), 0o755); err != nil {
		return err
	}
	if err := makeSymlink(expectedTarget, expectedAbs); err != nil {
		return err
	}
	state.Dirs[entry.Identity] = StateDir{Path: expectedRel, Link: dir.linkPath}
	fmt.Fprintf(out, "linked-dir: %s\n", dirPath)
	return nil
}

// removeEmptyDirTree removes path and its subdirectories bottom-up as far as
// they are empty; anything non-empty is left in place for the caller to
// inspect.
func removeEmptyDirTree(path string) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() {
			removeEmptyDirTree(filepath.Join(path, entry.Name()))
		}
	}
	_ = os.Remove(path)
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
	defer cmd.Wait()

	reader := tar.NewReader(stdout)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		rel := filepath.ToSlash(filepath.Clean(header.Name))
		if err := validateSafePath(rel); err != nil {
			return fmt.Errorf("unsafe path in source tree: %q", header.Name)
		}
		if _, claimed := manifest[rel]; claimed {
			counts.shadowed++
			continue
		}
		content, err := io.ReadAll(reader)
		if err != nil {
			return err
		}
		newHash := sha256Hex(content)
		manifest[rel] = newHash

		target := filepath.Join(dirAbs, filepath.FromSlash(rel))
		mode := header.FileInfo().Mode().Perm()
		if pathEntryExists(target) {
			// A symlink here cannot be a file this $dir wrote (manifests
			// track regular files only, and hashing through the link may
			// fail on loops); keep it like a locally modified file.
			if isSymlink(target) {
				counts.kept++
				continue
			}
			localHash, err := fileSHA256(target)
			if err != nil {
				return err
			}
			if localHash == newHash {
				counts.unchanged++
				continue
			}
			oldHash, tracked := oldManifest[rel]
			if !tracked || localHash != oldHash {
				counts.kept++
				continue
			}
			if err := os.WriteFile(target, content, mode); err != nil {
				return err
			}
			_ = os.Chmod(target, mode)
			counts.updated++
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(target, content, mode); err != nil {
			return err
		}
		counts.added++
	}
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("git archive: %s", err)
	}
	return nil
}

func installedDirIdentitySet(root string, model *Model, state *State) map[string]bool {
	installed := map[string]bool{}
	dirIdentityToPath := identityToPath(model, EntryDir)
	for identity, stateDir := range state.Dirs {
		if _, ok := dirIdentityToPath[identity]; !ok {
			continue
		}
		if pathExists(filepath.Join(root, stateDir.Path)) {
			installed[identity] = true
		}
	}
	return installed
}
