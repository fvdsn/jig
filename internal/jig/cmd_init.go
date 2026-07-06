package jig

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type InitOptions struct {
	SourceArg       string
	WorkspaceDir    string
	SchemaPath      string // schema path inside the source repo; probed when empty
	Clone           bool
	ClonePath       string
	IncludeOptional bool
	IncludeArchived bool
}

func Init(options InitOptions, out io.Writer) error {
	workspaceDir, err := filepath.Abs(options.WorkspaceDir)
	if err != nil {
		return err
	}
	if options.SchemaPath != "" {
		if err := validateSafePath(options.SchemaPath); err != nil {
			return fmt.Errorf("invalid schema path: %s", err)
		}
	}
	if pathExists(filepath.Join(workspaceDir, configFile)) {
		return errors.New("workspace already initialized: .jig/config.json exists")
	}
	sourceAbs := filepath.Join(workspaceDir, sourceDir)
	if pathExists(sourceAbs) {
		return fmt.Errorf("source checkout already exists: %s", sourceDir)
	}
	if err := os.MkdirAll(filepath.Dir(sourceAbs), 0o755); err != nil {
		return err
	}

	schemaPath, err := setUpSource(options.SourceArg, options.SchemaPath, sourceAbs)
	if err != nil {
		return err
	}

	def, err := loadDefinition(filepath.Join(sourceAbs, filepath.FromSlash(schemaPath)))
	if err != nil {
		return err
	}
	validation := validateDefinition(def)
	if len(validation.Errors) > 0 {
		return validation.asError("invalid schema")
	}
	model, err := flattenDefinition(def)
	if err != nil {
		return err
	}

	if err := saveConfig(workspaceDir, Config{Version: 1, Schema: schemaPath}); err != nil {
		return err
	}
	if err := saveState(workspaceDir, emptyState()); err != nil {
		return err
	}

	fmt.Fprintf(out, "initialized workspace at %s\n", workspaceDir)
	if options.Clone {
		ws := Workspace{Root: workspaceDir, Config: Config{Version: 1, Schema: schemaPath}, Def: *def, Model: model, State: emptyState()}
		cloneOptions := CloneOptions{
			Path:            options.ClonePath,
			IncludeOptional: options.IncludeOptional,
			IncludeArchived: options.IncludeArchived,
		}
		if err := clonePathIntoWorkspace(out, &ws, cloneOptions); err != nil {
			return err
		}
		if err := saveState(workspaceDir, ws.State); err != nil {
			return err
		}
	}
	return nil
}

// setUpSource creates the schema source checkout: a git clone when sourceArg
// is a repository, or a fresh git-initialized directory seeded with the given
// local schema file. It returns the schema path inside the checkout.
func setUpSource(sourceArg string, schemaPath string, sourceAbs string) (string, error) {
	info, err := os.Stat(sourceArg)
	if err == nil && !info.IsDir() {
		if schemaPath != "" {
			return "", errors.New("--path can only be used with Git sources")
		}
		data, err := os.ReadFile(sourceArg)
		if err != nil {
			return "", err
		}
		if _, err := git("", "init", "--quiet", sourceAbs); err != nil {
			return "", err
		}
		schemaPath = "jig.json"
		if err := os.WriteFile(filepath.Join(sourceAbs, schemaPath), data, 0o644); err != nil {
			return "", err
		}
		return schemaPath, nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	if err := cloneRepo(sourceArg, sourceAbs); err != nil {
		return "", err
	}
	if schemaPath != "" {
		if !pathExists(filepath.Join(sourceAbs, filepath.FromSlash(schemaPath))) {
			return "", fmt.Errorf("schema file %s not found in source repository", schemaPath)
		}
		return schemaPath, nil
	}
	for _, candidate := range schemaCandidates {
		if pathExists(filepath.Join(sourceAbs, candidate)) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("no schema file (%s) found at the root of the source repository; use --path", schemaCandidateList())
}

func schemaCandidateList() string {
	list := ""
	for i, candidate := range schemaCandidates {
		if i > 0 {
			list += ", "
		}
		list += candidate
	}
	return list
}
