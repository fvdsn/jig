package jig

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

const (
	configFile        = ".jig/config.json"
	stateFile         = ".jig/state.json"
	sourceDir         = ".jig/source"
	workspaceLockFile = ".jig/lock"
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
	Subdir string // where the command runs, relative to Root; "" at the root
	Config Config
	Def    Definition
	Model  Model
	State  State

	unlock    func()          // releases the workspace lock; set when loaded with state
	installed *InstalledNodes // memoized installedNodes scan; nil until asked
}

// Close releases the workspace lock. Commands that load the workspace with
// state must defer it.
func (ws *Workspace) Close() {
	if ws.unlock != nil {
		ws.unlock()
		ws.unlock = nil
	}
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
	subdir, err := workspaceSubdir(root, cwd)
	if err != nil {
		return nil, err
	}
	return loadWorkspaceAt(root, subdir, withState)
}

// loadWorkspaceAt loads the workspace rooted at root. Commands that do not
// run inside the workspace (init resuming an initialized directory) load it
// directly by path instead of finding the root from the current directory.
func loadWorkspaceAt(root string, subdir string, withState bool) (*Workspace, error) {
	config, err := loadConfig(root)
	if err != nil {
		return nil, err
	}
	ws := &Workspace{Root: root, Subdir: subdir, Config: config}
	if withState {
		// Commands that mutate state hold an exclusive lock so concurrent
		// jig runs cannot silently drop each other's updates.
		unlock, err := acquireLock(filepath.Join(root, workspaceLockFile), 10*time.Second)
		if err != nil {
			return nil, err
		}
		ws.unlock = unlock
	}
	def, err := loadDefinition(ws.SchemaFile())
	if err != nil {
		ws.Close()
		return nil, err
	}
	if err := schemaVersionError(def.Version); err != nil {
		ws.Close()
		return nil, err
	}
	model, err := flattenDefinition(def)
	if err != nil {
		ws.Close()
		return nil, err
	}
	ws.Def = *def
	ws.Model = model
	// A missing state file is an empty workspace; anything else (corrupt
	// JSON, a version from a newer jig) is an error for every command, or a
	// read-only command would report a fully installed workspace as empty.
	ws.State, err = loadState(root)
	if err != nil {
		ws.Close()
		return nil, err
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
		if pathExists(filepath.Join(dir, ".jig.json")) {
			return "", fmt.Errorf("found a legacy .jig.json at %s; that workspace layout is no longer supported, run jig init again", dir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("could not find a .jig workspace in current directory or parents; run jig init to create one")
		}
		dir = parent
	}
}

// workspaceSubdir reports where the command runs relative to the workspace
// root, in slash form. Inside .jig (editing the schema checkout, say) counts
// as the root.
func workspaceSubdir(root string, cwd string) (string, error) {
	rel, err := filepath.Rel(root, cwd)
	if err != nil {
		return "", err
	}
	subdir := filepath.ToSlash(rel)
	if subdir == "." || subdir == ".jig" || strings.HasPrefix(subdir, ".jig/") {
		return "", nil
	}
	return subdir, nil
}

// ResolvePath interprets a CLI path argument relative to the directory the
// command runs in: an empty path means the current subtree, "." and ".."
// work like filesystem paths, and a leading "/" anchors to the workspace
// root. The result is a workspace path ("" for the whole workspace).
func (ws *Workspace) ResolvePath(arg string) (string, error) {
	var resolved string
	if strings.HasPrefix(arg, "/") {
		resolved = path.Clean(strings.TrimPrefix(arg, "/"))
	} else if arg == "" {
		return ws.Subdir, nil
	} else {
		resolved = path.Join(ws.Subdir, arg)
	}
	if resolved == "." || resolved == "" {
		return "", nil
	}
	if resolved == ".." || strings.HasPrefix(resolved, "../") {
		return "", fmt.Errorf("path %q is outside the workspace", arg)
	}
	return resolved, nil
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
	if config.Version > 1 {
		return Config{}, fmt.Errorf("this workspace was created by a newer jig (config version %d); upgrade jig", config.Version)
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
	discard := func(err error) error {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return discard(err)
	}
	if err := tmp.Chmod(0o644); err != nil {
		return discard(err)
	}
	if err := tmp.Close(); err != nil {
		return discard(err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return discard(err)
	}
	return nil
}
