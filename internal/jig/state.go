package jig

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type State struct {
	Version int                  `json:"version"`
	Repos   map[string]StateRepo `json:"repos"`
	Files   map[string]StateFile `json:"files"`
	Dirs    map[string]StateDir  `json:"dirs,omitempty"`
}

// StateDir tracks a materialized subtree: the source tree id and a manifest
// of every file written (relative path to sha256), which is what makes
// updates and deletions safe.
type StateDir struct {
	Path  string            `json:"path"`
	Src   string            `json:"src,omitempty"`
	Link  string            `json:"link,omitempty"`
	Tree  string            `json:"tree,omitempty"`
	Files map[string]string `json:"files,omitempty"`
}

type StateRepo struct {
	Path string `json:"path"`
	Git  string `json:"git,omitempty"`
}

type StateFile struct {
	Path    string `json:"path"`
	Src     string `json:"src,omitempty"`
	Link    string `json:"link,omitempty"`
	SHA256  string `json:"sha256,omitempty"`
	SrcBlob string `json:"srcBlob,omitempty"` // git blob id of the source file when written
}

func emptyState() State {
	return State{Version: 1, Repos: map[string]StateRepo{}, Files: map[string]StateFile{}, Dirs: map[string]StateDir{}}
}

func loadState(root string) (State, error) {
	path := filepath.Join(root, stateFile)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return emptyState(), nil
	}
	if err != nil {
		return State{}, err
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return State{}, err
	}
	if state.Version > 1 {
		// Rewriting newer state would silently strip fields this jig does
		// not know about.
		return State{}, fmt.Errorf("the workspace state uses version %d, which this jig does not understand; upgrade jig", state.Version)
	}
	if state.Version == 0 {
		state.Version = 1
	}
	if state.Repos == nil {
		state.Repos = map[string]StateRepo{}
	}
	if state.Files == nil {
		state.Files = map[string]StateFile{}
	}
	if state.Dirs == nil {
		state.Dirs = map[string]StateDir{}
	}
	return state, nil
}

func saveState(root string, state State) error {
	if state.Version == 0 {
		state.Version = 1
	}
	if state.Repos == nil {
		state.Repos = map[string]StateRepo{}
	}
	if state.Files == nil {
		state.Files = map[string]StateFile{}
	}
	if state.Dirs == nil {
		state.Dirs = map[string]StateDir{}
	}
	return writeJSON(filepath.Join(root, stateFile), &state)
}

// stateKinds are the kinds state tracks, in report order.
var stateKinds = []EntryKind{EntryRepo, EntryFile, EntryDir}

// records returns identity -> recorded path for every record of the kind:
// the view the cross-kind bookkeeping (stale detection, readoption,
// pruning, status) works on, so it is written once instead of per kind.
func (state *State) records(kind EntryKind) map[string]string {
	paths := map[string]string{}
	switch kind {
	case EntryRepo:
		for identity, record := range state.Repos {
			paths[identity] = record.Path
		}
	case EntryFile:
		for identity, record := range state.Files {
			paths[identity] = record.Path
		}
	case EntryDir:
		for identity, record := range state.Dirs {
			paths[identity] = record.Path
		}
	}
	return paths
}

// drop removes the record of an identity.
func (state *State) drop(kind EntryKind, identity string) {
	switch kind {
	case EntryRepo:
		delete(state.Repos, identity)
	case EntryFile:
		delete(state.Files, identity)
	case EntryDir:
		delete(state.Dirs, identity)
	}
}

// adopt moves the record of oldID under newID. When newID already has a
// record the old one is a leftover duplicate and is just dropped.
func (state *State) adopt(kind EntryKind, oldID string, newID string) {
	switch kind {
	case EntryRepo:
		record := state.Repos[oldID]
		delete(state.Repos, oldID)
		if _, taken := state.Repos[newID]; !taken {
			state.Repos[newID] = record
		}
	case EntryFile:
		record := state.Files[oldID]
		delete(state.Files, oldID)
		if _, taken := state.Files[newID]; !taken {
			state.Files[newID] = record
		}
	case EntryDir:
		record := state.Dirs[oldID]
		delete(state.Dirs, oldID)
		if _, taken := state.Dirs[newID]; !taken {
			state.Dirs[newID] = record
		}
	}
}

// recordInstalled reports whether a record of the kind is present on disk
// at its recorded path: a checkout for a repo, any entry (a dangling link
// included, it is still jig's to remove) for a file or dir.
func recordInstalled(root string, kind EntryKind, rel string) bool {
	abs := filepath.Join(root, rel)
	if kind == EntryRepo {
		return isGitRepo(abs)
	}
	return pathEntryExists(abs)
}

// reportStale reports state entries that are no longer defined in the schema
// and prunes the ones whose checkout or file is gone from disk too.
func reportStale(out io.Writer, root string, model *Model, state *State) {
	var stale []string
	var pruned []string
	for _, kind := range stateKinds {
		defined := identityToPath(model, kind)
		for identity, rel := range state.records(kind) {
			if _, ok := defined[identity]; ok {
				continue
			}
			if recordInstalled(root, kind, rel) {
				stale = append(stale, fmt.Sprintf("%s at %s is no longer defined", identity, rel))
			} else {
				state.drop(kind, identity)
				pruned = append(pruned, fmt.Sprintf("%s (no longer defined, not installed)", identity))
			}
		}
	}
	printGroup(out, "stale", stale)
	printGroup(out, "pruned", pruned)
}
