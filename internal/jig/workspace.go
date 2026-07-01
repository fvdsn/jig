package jig

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	configFile           = ".jig/config.json"
	stateFile            = ".jig/state.json"
	sourceDir            = ".jig/source"
	legacyDefinitionFile = ".jig.json"
)

// schemaCandidates are the schema file names probed at the root of a source
// repository when no explicit path is given, in order of preference.
var schemaCandidates = []string{".jig.json", "jig.json", "schema.json"}

type Config struct {
	Version int    `json:"version"`
	Schema  string `json:"schema"` // schema file path inside the source checkout
}

type Workspace struct {
	Root   string
	Config Config
	Def    Definition
	Model  Model
	State  State
}

// SchemaFile returns the absolute path of the live schema file inside the
// workspace's source checkout.
func (ws *Workspace) SchemaFile() string {
	return filepath.Join(ws.Root, sourceDir, filepath.FromSlash(ws.Config.Schema))
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
	config, err := loadConfig(root)
	if err != nil {
		return nil, err
	}
	ws := &Workspace{Root: root, Config: config}
	def, err := loadDefinition(ws.SchemaFile())
	if err != nil {
		return nil, err
	}
	model, err := flattenDefinition(def)
	if err != nil {
		return nil, err
	}
	ws.Def = *def
	ws.Model = model
	ws.State, err = loadState(root)
	if err != nil {
		if withState {
			return nil, err
		}
		ws.State = emptyState()
	}
	return ws, nil
}

func findWorkspace(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		if pathExists(filepath.Join(dir, configFile)) {
			return dir, nil
		}
		if pathExists(filepath.Join(dir, legacyDefinitionFile)) {
			return "", fmt.Errorf("found legacy .jig.json at %s; this layout is no longer supported, delete it and re-run jig init", dir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("could not find a .jig workspace in current directory or parents")
		}
		dir = parent
	}
}

func loadConfig(root string) (Config, error) {
	data, err := os.ReadFile(filepath.Join(root, configFile))
	if err != nil {
		return Config{}, err
	}
	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return Config{}, err
	}
	if config.Schema == "" {
		return Config{}, errors.New("workspace config is missing the schema path")
	}
	if err := validateSafePath(config.Schema); err != nil {
		return Config{}, fmt.Errorf("invalid schema path in workspace config: %s", err)
	}
	return config, nil
}

func saveConfig(root string, config Config) error {
	return writeJSON(filepath.Join(root, configFile), &config)
}

// writeJSON writes atomically (temp file + rename) so an interrupted write
// can never leave a truncated state or config file behind.
func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}
