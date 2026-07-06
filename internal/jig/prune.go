package jig

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// readoptRenamedIdentities transfers state records whose identity is no
// longer defined to the defined entry of the same kind expected at the same
// path. This is what a schema id rename looks like locally: the content is
// not stale, only its identity changed, so the record (origin URL, file
// hash, dir manifest) follows it instead of being reported stale.
func readoptRenamedIdentities(out io.Writer, model *Model, state *State) {
	var messages []string
	messages = append(messages, readoptRepos(model, state)...)
	messages = append(messages, readoptFiles(model, state)...)
	messages = append(messages, readoptDirs(model, state)...)
	printGroup(out, "readopted", messages)
}

// definedPathToIdentity maps each expected path of a defined entry of the
// given kind to its identity.
func definedPathToIdentity(model *Model, kind EntryKind) map[string]string {
	result := map[string]string{}
	for path, entry := range model.Entries {
		if entry.Kind == kind {
			result[path] = entry.Identity
		}
	}
	return result
}

func readoptRepos(model *Model, state *State) []string {
	defined := identityToPath(model, EntryRepo)
	pathToID := definedPathToIdentity(model, EntryRepo)
	var messages []string
	moves := map[string]string{}
	for identity, stateRepo := range state.Repos {
		if _, ok := defined[identity]; ok {
			continue
		}
		newID, ok := pathToID[stateRepo.Path]
		if !ok {
			continue
		}
		if existing, taken := state.Repos[newID]; taken {
			// An earlier sync already adopted the checkout under the new
			// identity; the old record is a leftover duplicate.
			if existing.Path == stateRepo.Path {
				moves[identity] = newID
			}
			continue
		}
		moves[identity] = newID
	}
	for oldID, newID := range moves {
		record := state.Repos[oldID]
		delete(state.Repos, oldID)
		if _, taken := state.Repos[newID]; !taken {
			state.Repos[newID] = record
		}
		messages = append(messages, fmt.Sprintf("%s (%s -> %s)", record.Path, oldID, newID))
	}
	return messages
}

func readoptFiles(model *Model, state *State) []string {
	defined := identityToPath(model, EntryFile)
	pathToID := definedPathToIdentity(model, EntryFile)
	var messages []string
	moves := map[string]string{}
	for identity, stateFile := range state.Files {
		if _, ok := defined[identity]; ok {
			continue
		}
		newID, ok := pathToID[stateFile.Path]
		if !ok {
			continue
		}
		if existing, taken := state.Files[newID]; taken {
			if existing.Path == stateFile.Path {
				moves[identity] = newID
			}
			continue
		}
		moves[identity] = newID
	}
	for oldID, newID := range moves {
		record := state.Files[oldID]
		delete(state.Files, oldID)
		if _, taken := state.Files[newID]; !taken {
			state.Files[newID] = record
		}
		messages = append(messages, fmt.Sprintf("%s (%s -> %s)", record.Path, oldID, newID))
	}
	return messages
}

func readoptDirs(model *Model, state *State) []string {
	defined := identityToPath(model, EntryDir)
	pathToID := definedPathToIdentity(model, EntryDir)
	var messages []string
	moves := map[string]string{}
	for identity, stateDir := range state.Dirs {
		if _, ok := defined[identity]; ok {
			continue
		}
		newID, ok := pathToID[stateDir.Path]
		if !ok {
			continue
		}
		if existing, taken := state.Dirs[newID]; taken {
			if existing.Path == stateDir.Path {
				moves[identity] = newID
			}
			continue
		}
		moves[identity] = newID
	}
	for oldID, newID := range moves {
		record := state.Dirs[oldID]
		delete(state.Dirs, oldID)
		if _, taken := state.Dirs[newID]; !taken {
			state.Dirs[newID] = record
		}
		messages = append(messages, fmt.Sprintf("%s (%s -> %s)", record.Path, oldID, newID))
	}
	return messages
}

// pruneStale deletes state-tracked entries that are no longer defined in the
// schema, with jig rm's data-safety rules: dirty or unpushed repositories,
// repositories whose origin no longer matches the recorded URL, locally
// modified files, and files the user added or edited inside dirs are kept
// and reported. A path owned by a defined entry is never deleted; only its
// obsolete state record is dropped.
func pruneStale(out io.Writer, root string, model *Model, state *State) {
	var pruned, kept []string
	owned := definedEntryPaths(model, state)

	definedRepos := identityToPath(model, EntryRepo)
	for identity, stateRepo := range state.Repos {
		if _, ok := definedRepos[identity]; ok {
			continue
		}
		abs := filepath.Join(root, stateRepo.Path)
		if !isGitRepo(abs) {
			delete(state.Repos, identity)
			pruned = append(pruned, identity+" (no longer defined, not installed)")
			continue
		}
		if owned[stateRepo.Path] {
			delete(state.Repos, identity)
			pruned = append(pruned, fmt.Sprintf("%s (state only: %s is owned by a defined entry)", identity, stateRepo.Path))
			continue
		}
		if origin, err := gitOrigin(abs); err != nil || origin != stateRepo.Git {
			kept = append(kept, stateRepo.Path+": origin does not match the recorded URL")
			continue
		}
		if isDirty(abs) {
			kept = append(kept, stateRepo.Path+": uncommitted changes")
			continue
		}
		if reason := unpushedReason(abs); reason != "" {
			kept = append(kept, stateRepo.Path+": "+reason)
			continue
		}
		if err := os.RemoveAll(abs); err != nil {
			kept = append(kept, stateRepo.Path+": "+err.Error())
			continue
		}
		pruneEmptyParents(root, filepath.Dir(stateRepo.Path))
		delete(state.Repos, identity)
		pruned = append(pruned, stateRepo.Path)
	}

	definedFiles := identityToPath(model, EntryFile)
	for identity, stateFile := range state.Files {
		if _, ok := definedFiles[identity]; ok {
			continue
		}
		abs := filepath.Join(root, stateFile.Path)
		if !pathEntryExists(abs) {
			delete(state.Files, identity)
			pruned = append(pruned, identity+" (no longer defined, not installed)")
			continue
		}
		if owned[stateFile.Path] {
			delete(state.Files, identity)
			pruned = append(pruned, fmt.Sprintf("%s (state only: %s is owned by a defined entry)", identity, stateFile.Path))
			continue
		}
		// Symlinks are tracked link files; removing one loses only the link.
		if stateFile.SHA256 != "" && !isSymlink(abs) {
			hash, err := fileSHA256(abs)
			if err != nil || hash != stateFile.SHA256 {
				kept = append(kept, stateFile.Path+": locally modified")
				continue
			}
		}
		if err := os.Remove(abs); err != nil {
			kept = append(kept, stateFile.Path+": "+err.Error())
			continue
		}
		pruneEmptyParents(root, filepath.Dir(stateFile.Path))
		delete(state.Files, identity)
		pruned = append(pruned, stateFile.Path)
	}

	definedDirs := identityToPath(model, EntryDir)
	for identity, stateDir := range state.Dirs {
		if _, ok := definedDirs[identity]; ok {
			continue
		}
		abs := filepath.Join(root, stateDir.Path)
		if !pathEntryExists(abs) {
			delete(state.Dirs, identity)
			pruned = append(pruned, identity+" (no longer defined, not installed)")
			continue
		}
		if owned[stateDir.Path] {
			delete(state.Dirs, identity)
			pruned = append(pruned, fmt.Sprintf("%s (state only: %s is owned by a defined entry)", identity, stateDir.Path))
			continue
		}
		if err := pruneStaleDir(root, stateDir); err != nil {
			kept = append(kept, stateDir.Path+": "+err.Error())
			continue
		}
		delete(state.Dirs, identity)
		pruned = append(pruned, stateDir.Path)
	}

	printGroup(out, "pruned", pruned)
	printGroup(out, "kept", kept)
}

// pruneStaleDir deletes a stale $dir like removeDir does: the symlink for a
// link dir, otherwise the manifest-tracked files, keeping everything the
// user added or modified.
func pruneStaleDir(root string, stateDir StateDir) error {
	abs := filepath.Join(root, stateDir.Path)
	if stateDir.Link != "" {
		if isSymlink(abs) {
			if err := os.Remove(abs); err != nil {
				return err
			}
			pruneEmptyParents(root, filepath.Dir(stateDir.Path))
		}
		return nil
	}
	modified := 0
	for fileRel, recorded := range stateDir.Files {
		target := filepath.Join(abs, filepath.FromSlash(fileRel))
		if isSymlink(target) {
			modified++
			continue
		}
		if hash, err := fileSHA256(target); err == nil && hash != recorded {
			modified++
		}
	}
	if modified > 0 {
		return fmt.Errorf("%d locally modified files", modified)
	}
	for fileRel := range stateDir.Files {
		target := filepath.Join(abs, filepath.FromSlash(fileRel))
		if pathEntryExists(target) {
			if err := os.Remove(target); err != nil {
				return err
			}
		}
		pruneEmptyParents(root, filepath.Dir(filepath.Join(stateDir.Path, filepath.FromSlash(fileRel))))
	}
	_ = os.Remove(abs)
	pruneEmptyParents(root, filepath.Dir(stateDir.Path))
	return nil
}

// definedEntryPaths returns every workspace path currently owned by a
// defined entry: the schema's expected paths plus the recorded paths of
// state entries whose identity is still defined (moves not yet applied).
func definedEntryPaths(model *Model, state *State) map[string]bool {
	owned := map[string]bool{}
	for path, entry := range model.Entries {
		switch entry.Kind {
		case EntryRepo, EntryFile, EntryDir:
			owned[path] = true
		}
	}
	definedRepos := identityToPath(model, EntryRepo)
	for identity, stateRepo := range state.Repos {
		if _, ok := definedRepos[identity]; ok {
			owned[stateRepo.Path] = true
		}
	}
	definedFiles := identityToPath(model, EntryFile)
	for identity, stateFile := range state.Files {
		if _, ok := definedFiles[identity]; ok {
			owned[stateFile.Path] = true
		}
	}
	definedDirs := identityToPath(model, EntryDir)
	for identity, stateDir := range state.Dirs {
		if _, ok := definedDirs[identity]; ok {
			owned[stateDir.Path] = true
		}
	}
	return owned
}
