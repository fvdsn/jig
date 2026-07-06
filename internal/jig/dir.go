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
func ensureDir(out io.Writer, root string, model *Model, state *State, dirPath string, allowMove bool, refresh bool, fetcher *fileFetcher, activeRepos map[string]bool, installedRepos map[string]bool) error {
	entry, _ := model.entry(dirPath, EntryDir)
	dir := entry.Dir
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

	// Resolve every source before touching the workspace, so a partial
	// materialization cannot happen when a later source is broken.
	type resolvedSource struct {
		mirror string
		tree   string
	}
	var sources []resolvedSource
	var treeOIDs []string
	var activeSrcs []string
	for _, dirSource := range dir.Src {
		// A per-source onlyWhen gates just this source's tree in the merge.
		if dirSource.OnlyWhen != nil && !conditionMatches(*dirSource.OnlyWhen, activeRepos, installedRepos, model) {
			continue
		}
		parsed, err := parseDirSrc(dirSource.Src)
		if err != nil {
			return err
		}
		mirror, err := fetcher.mirror(parsed.GitURL)
		if err != nil {
			if hasState && pathExists(expectedAbs) {
				fmt.Fprintf(out, "present-dir: %s (source not checked: %s)\n", dirPath, shortError(err))
				return nil
			}
			return err
		}
		treeRef := "HEAD^{tree}"
		if parsed.Path != "" {
			treeRef = "HEAD:" + parsed.Path
		}
		treeOut, err := git(mirror, "rev-parse", treeRef)
		if err != nil {
			return fmt.Errorf("source subtree not found: %s", shortError(err))
		}
		treeOID := strings.TrimSpace(treeOut)
		if objType, err := git(mirror, "cat-file", "-t", treeOID); err != nil || strings.TrimSpace(objType) != "tree" {
			return fmt.Errorf("source path %s is not a directory in the source repository", parsed.Path)
		}
		sources = append(sources, resolvedSource{mirror, treeOID})
		treeOIDs = append(treeOIDs, treeOID)
		activeSrcs = append(activeSrcs, dirSource.Src)
	}
	srcKey := strings.Join(activeSrcs, " ")
	combinedTree := strings.Join(treeOIDs, "+")

	if hasState && !refresh && stateDir.Src == srcKey && stateDir.Tree == combinedTree && manifestClean(expectedAbs, stateDir.Files) {
		fmt.Fprintf(out, "present-dir: %s\n", dirPath)
		return nil
	}

	oldManifest := map[string]string{}
	if hasState {
		oldManifest = stateDir.Files
	}
	newManifest := map[string]string{}
	var counts dirCounts
	for _, source := range sources {
		if err := materializeTree(source.mirror, source.tree, expectedAbs, oldManifest, newManifest, &counts); err != nil {
			return err
		}
	}

	// Files that disappeared upstream: delete only untouched ones.
	for rel, oldHash := range oldManifest {
		if _, stillThere := newManifest[rel]; stillThere {
			continue
		}
		target := filepath.Join(expectedAbs, filepath.FromSlash(rel))
		if !pathEntryExists(target) {
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
	fmt.Fprintln(out, dirMessage(dirPath, hasState, counts))
	return nil
}

type dirCounts struct {
	added     int
	updated   int
	unchanged int
	kept      int // locally modified files that were not overwritten
	deleted   int // removed because they vanished upstream and were untouched
	abandoned int // vanished upstream but locally modified; left as untracked
	shadowed  int // provided by more than one source; the first source won
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
		hash, err := fileSHA256(filepath.Join(dirAbs, filepath.FromSlash(rel)))
		if err != nil || hash != recorded {
			return false
		}
	}
	return true
}

// materializeTree streams `git archive` of the tree from the mirror into
// dirAbs, merging into manifest. Files already claimed by an earlier source
// are shadowed; files matching the old manifest (untouched) are overwritten;
// locally modified files are kept and counted.
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
