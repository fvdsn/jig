package jig

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

const (
	definitionFile = ".jig.json"
	stateFile      = ".jig/state.json"
)

const DefaultDefinitionPath = definitionFile

type Workspace struct {
	Root  string
	Def   Definition
	Model Model
	State State
}

func loadWorkspace(withState bool) (*Workspace, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	root, err := findWorkspace(cwd)
	if err != nil {
		return nil, err
	}
	def, err := loadDefinition(filepath.Join(root, definitionFile))
	if err != nil {
		return nil, err
	}
	model, err := flattenDefinition(def)
	if err != nil {
		return nil, err
	}
	state := emptyState()
	if withState {
		state, err = loadState(root)
		if err != nil {
			return nil, err
		}
	} else {
		state, _ = loadState(root)
	}
	return &Workspace{Root: root, Def: *def, Model: model, State: state}, nil
}

func findWorkspace(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, definitionFile)); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("could not find .jig.json in current directory or parents")
		}
		dir = parent
	}
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
