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
}

type StateRepo struct {
	Path string `json:"path"`
	Git  string `json:"git,omitempty"`
}

type StateFile struct {
	Path   string `json:"path"`
	Src    string `json:"src,omitempty"`
	Link   string `json:"link,omitempty"`
	SHA256 string `json:"sha256,omitempty"`
}

func emptyState() State {
	return State{Version: 1, Repos: map[string]StateRepo{}, Files: map[string]StateFile{}}
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
	if state.Version == 0 {
		state.Version = 1
	}
	if state.Repos == nil {
		state.Repos = map[string]StateRepo{}
	}
	if state.Files == nil {
		state.Files = map[string]StateFile{}
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
	return writeJSON(filepath.Join(root, stateFile), &state)
}

// reportStale reports state entries that are no longer defined in the schema
// and prunes the ones whose checkout or file is gone from disk too.
func reportStale(out io.Writer, root string, model *Model, state *State) {
	repoIdentityToPath := repoIdentityToPath(model)
	fileIdentityToPath := fileIdentityToPath(model)
	var stale []string
	var pruned []string
	for identity, stateRepo := range state.Repos {
		if _, ok := repoIdentityToPath[identity]; ok {
			continue
		}
		if isGitRepo(filepath.Join(root, stateRepo.Path)) {
			stale = append(stale, fmt.Sprintf("%s at %s is no longer defined", identity, stateRepo.Path))
		} else {
			delete(state.Repos, identity)
			pruned = append(pruned, fmt.Sprintf("%s (no longer defined, not installed)", identity))
		}
	}
	for identity, stateFile := range state.Files {
		if _, ok := fileIdentityToPath[identity]; ok {
			continue
		}
		if pathExists(filepath.Join(root, stateFile.Path)) {
			stale = append(stale, fmt.Sprintf("%s at %s is no longer defined", identity, stateFile.Path))
		} else {
			delete(state.Files, identity)
			pruned = append(pruned, fmt.Sprintf("%s (no longer defined, not installed)", identity))
		}
	}
	printGroup(out, "stale", stale)
	printGroup(out, "pruned", pruned)
}
