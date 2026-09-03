package jig

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"
)

// readoptRenamedIdentities transfers state records whose identity is no
// longer defined to the defined entry of the same kind expected at the same
// path. This is what a schema id rename looks like locally: the content is
// not stale, only its identity changed, so the record (origin URL, file
// hash, dir manifest) follows it instead of being reported stale.
func readoptRenamedIdentities(out io.Writer, model *Model, state *State) {
	var messages []string
	for _, kind := range stateKinds {
		messages = append(messages, readoptKind(model, state, kind)...)
	}
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

func readoptKind(model *Model, state *State, kind EntryKind) []string {
	defined := identityToPath(model, kind)
	pathToID := definedPathToIdentity(model, kind)
	records := state.records(kind)
	moves := map[string]string{}
	for identity, rel := range records {
		if _, ok := defined[identity]; ok {
			continue
		}
		newID, ok := pathToID[rel]
		if !ok {
			continue
		}
		// An earlier sync may already have adopted the checkout under the
		// new identity; then the old record is a leftover duplicate only
		// when it names the same path.
		if existingPath, taken := records[newID]; taken && existingPath != rel {
			continue
		}
		moves[identity] = newID
	}
	var messages []string
	for oldID, newID := range moves {
		state.adopt(kind, oldID, newID)
		messages = append(messages, fmt.Sprintf("%s (%s -> %s)", records[oldID], oldID, newID))
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
	for _, kind := range stateKinds {
		defined := identityToPath(model, kind)
		for identity, rel := range state.records(kind) {
			if _, ok := defined[identity]; ok {
				continue
			}
			if !recordInstalled(root, kind, rel) {
				state.drop(kind, identity)
				pruned = append(pruned, identity+" (no longer defined, not installed)")
				continue
			}
			if owned[rel] {
				state.drop(kind, identity)
				pruned = append(pruned, fmt.Sprintf("%s (state only: %s is owned by a defined entry)", identity, rel))
				continue
			}
			if err := deleteStaleRecord(root, state, kind, identity); err != nil {
				kept = append(kept, rel+": "+err.Error())
				continue
			}
			state.drop(kind, identity)
			pruned = append(pruned, rel)
		}
	}
	printGroup(out, "pruned", pruned)
	printGroup(out, "kept", kept)
}

// deleteStaleRecord takes a stale record off disk under the shared safety
// rules. A repository is additionally kept when its origin no longer matches
// the recorded URL: it is not the checkout the record describes.
func deleteStaleRecord(root string, state *State, kind EntryKind, identity string) error {
	switch kind {
	case EntryRepo:
		record := state.Repos[identity]
		if origin, err := gitOrigin(filepath.Join(root, record.Path)); err != nil || origin != record.Git {
			return errors.New("origin does not match the recorded URL")
		}
		return deleteTrackedRepo(root, record.Path, false)
	case EntryFile:
		record := state.Files[identity]
		_, err := deleteTrackedFile(root, record.Path, record, refuseModified)
		return err
	case EntryDir:
		record := state.Dirs[identity]
		_, err := deleteTrackedDir(root, record.Path, record, refuseModified)
		return err
	}
	return nil
}

// definedEntryPaths returns every workspace path currently owned by a
// defined entry: the schema's expected paths plus the recorded paths of
// state entries whose identity is still defined (moves not yet applied).
func definedEntryPaths(model *Model, state *State) map[string]bool {
	owned := map[string]bool{}
	for path, entry := range model.Entries {
		if entry.Kind != EntryGroup {
			owned[path] = true
		}
	}
	for _, kind := range stateKinds {
		defined := identityToPath(model, kind)
		for identity, rel := range state.records(kind) {
			if _, ok := defined[identity]; ok {
				owned[rel] = true
			}
		}
	}
	return owned
}
