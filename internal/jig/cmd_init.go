package jig

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type InitOptions struct {
	SourceArg       string // git URL or local schema file; empty starts a fresh workspace
	WorkspaceDir    string
	SchemaPath      string // schema path inside the source repo; probed when empty
	Clone           bool
	ClonePath       string
	IncludeOptional bool
	IncludeArchived bool
}

// sampleSchema seeds a bare "jig init": a starter schema whose only entry
// pulls the official jig skill, so coding agents in the fresh workspace
// already know how to drive and evolve it.
const sampleSchema = `{
  "version": 1,
  "tree": {
    ".agents/skills": {
      "$dir": {
        "id": "agent-skills",
        "description": "Skills for AI agents working in this workspace",
        "src": ["https://github.com/fvdsn/jig/tree/master/.agents/skills"]
      }
    }
  }
}
`

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
		cloneErr := clonePathIntoWorkspace(out, &ws, cloneOptions)
		// State accumulated before a clone error is valid and must be kept.
		if err := saveState(workspaceDir, ws.State); err != nil {
			return err
		}
		if cloneErr != nil {
			// A fresh workspace is usable even when fetching the starter
			// skill fails (offline); jig sync retries later.
			if options.SourceArg != "" {
				return cloneErr
			}
			fmt.Fprintf(out, "could not fetch everything (%s); run jig sync to retry\n", shortError(cloneErr))
		}
	}
	if options.SourceArg == "" {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "next steps:")
		fmt.Fprintln(out, "  $EDITOR .jig/source/jig.json   describe your repositories")
		fmt.Fprintln(out, "  jig validate                   check the schema")
		fmt.Fprintln(out, "  jig sync                       apply it to the workspace")
		fmt.Fprintln(out, "to share the workspace, push .jig/source to a Git remote; teammates run: jig init <url>")
	}
	return nil
}

// setUpSource creates the schema source checkout: a git clone when sourceArg
// is a repository, or a fresh git-initialized directory seeded with a local
// schema file (the starter schema when sourceArg is empty). It returns the
// schema path inside the checkout.
func setUpSource(sourceArg string, schemaPath string, sourceAbs string) (string, error) {
	if sourceArg == "" {
		if schemaPath != "" {
			return "", errors.New("--path can only be used with Git sources")
		}
		return seedSource(sourceAbs, []byte(sampleSchema))
	}
	info, err := os.Stat(sourceArg)
	if err == nil && !info.IsDir() {
		if schemaPath != "" {
			return "", errors.New("--path can only be used with Git sources")
		}
		data, err := os.ReadFile(sourceArg)
		if err != nil {
			return "", err
		}
		return seedSource(sourceAbs, data)
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

// seedSource creates the source checkout as a fresh git repository seeded
// with the given schema content as jig.json.
func seedSource(sourceAbs string, schema []byte) (string, error) {
	if _, err := git("", "init", "--quiet", sourceAbs); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(sourceAbs, "jig.json"), schema, 0o644); err != nil {
		return "", err
	}
	return "jig.json", nil
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
