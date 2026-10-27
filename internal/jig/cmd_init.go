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
	SkipDeps        bool // clone only the selected repos, without their dependencies
	Tags            []string
	Harnesses       *[]string // --harness: record this list (empty for none); nil leaves detection on
	Switch          string    // --switch: after the clone, switch every installed repository to this branch, creating it where missing
}

// sampleSchema seeds a bare "jig init": a starter schema whose only
// declaration pulls the official jig skill, so coding agents in the fresh
// workspace already know how to drive and evolve it. It is YAML, the
// format for a schema people write by hand.
const sampleSchema = `# The workspace schema: repositories and support files under tree, agent
# context under agents. Run jig skill for the full reference.
version: 4

tree: {}
`

const sampleSchemaName = "jig.yaml"

func Init(options InitOptions, out io.Writer) error {
	if options.Switch != "" {
		if !options.Clone {
			return errors.New("--switch needs --clone: there is nothing to switch before anything is installed")
		}
		if _, err := git("", "check-ref-format", "--branch", options.Switch); err != nil {
			return fmt.Errorf("invalid branch name %q", options.Switch)
		}
	}
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
		return resumeInit(workspaceDir, options, out)
	}
	sourceAbs := filepath.Join(workspaceDir, sourceDir)
	// A source checkout without config.json is a leftover from an init that
	// failed midway: the config is written only once the source and schema
	// are good. Replace it, so reinitializing converges instead of erroring
	// (and picks up upstream fixes a stale leftover would hide).
	if pathExists(sourceAbs) {
		if err := os.RemoveAll(sourceAbs); err != nil {
			return err
		}
		fmt.Fprintf(out, "replacing source checkout left by a failed init: %s\n", sourceDir)
	}
	// Until the config is written, a failure removes what this init
	// created, so a typo in the URL leaves no stray workspace behind.
	undo := undoInit(workspaceDir, sourceAbs)
	if err := os.MkdirAll(filepath.Dir(sourceAbs), 0o755); err != nil {
		undo()
		return err
	}
	config, err := setUpInitSource(options, sourceAbs)
	if err == nil {
		err = saveConfig(workspaceDir, config)
	}
	if err != nil {
		undo()
		return err
	}
	if err := saveState(workspaceDir, emptyState()); err != nil {
		return err
	}

	fmt.Fprintf(out, "initialized workspace at %s\n", workspaceDir)
	reportHarnesses(out, config)
	if options.Clone {
		ws, err := loadWorkspaceAt(workspaceDir, "", true)
		if err != nil {
			return err
		}
		defer ws.Close()
		if cloneErr := cloneIntoWorkspace(ws, options, out); cloneErr != nil {
			// A fresh workspace is usable even when fetching the starter
			// skill fails (offline); jig sync retries later.
			if options.SourceArg != "" {
				return cloneErr
			}
			fmt.Fprintf(out, "could not fetch everything (%s); run jig sync to retry\n", shortError(cloneErr))
		}
		if err := switchAfterInit(ws, options, out); err != nil {
			return err
		}
	}
	if options.SourceArg == "" {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "next steps:")
		fmt.Fprintf(out, "  $EDITOR .jig/source/%s   describe your repositories\n", config.Schema)
		fmt.Fprintln(out, "  jig validate                   check the schema")
		fmt.Fprintln(out, "  jig sync                       apply it to the workspace")
		fmt.Fprintln(out, "to share the workspace, push .jig/source to a Git remote; teammates run: jig init <url>")
	}
	return nil
}

// setUpInitSource sets up the schema checkout and checks the schema, and
// returns the config to save for it.
func setUpInitSource(options InitOptions, sourceAbs string) (Config, error) {
	schemaPath, err := setUpSource(options.SourceArg, options.SchemaPath, sourceAbs)
	if err != nil {
		return Config{}, err
	}
	def, err := loadDefinition(filepath.Join(sourceAbs, filepath.FromSlash(schemaPath)))
	if err != nil {
		return Config{}, err
	}
	validation := validateDefinition(def)
	if len(validation.Errors) > 0 {
		return Config{}, validation.asError("invalid schema")
	}
	config := Config{Version: 1, Schema: schemaPath, Harnesses: options.Harnesses}
	if _, err := flattenDefinition(def, harnessesOf(config)); err != nil {
		return Config{}, err
	}
	return config, nil
}

// undoInit returns a cleanup for a failed init: it removes the workspace
// directory if init is about to create it, else the .jig directory if init
// is about to create that, else only the source checkout.
func undoInit(workspaceDir string, sourceAbs string) func() {
	target := sourceAbs
	if !pathExists(workspaceDir) {
		target = workspaceDir
	} else if !pathExists(filepath.Dir(sourceAbs)) {
		target = filepath.Dir(sourceAbs)
	}
	return func() { _ = os.RemoveAll(target) }
}

// resumeInit finishes the setup of an already-initialized workspace: a rerun
// of jig init that targets the same source converges — updating the schema
// and running the clone step — instead of directing the user to separate
// commands. A different source is a conflict and refuses; so do sources
// without a verifiable identity (bare init, local schema files).
func resumeInit(workspaceDir string, options InitOptions, out io.Writer) error {
	if !isGitSource(options.SourceArg) {
		return errors.New("workspace already initialized: .jig/config.json exists")
	}
	ws, err := loadWorkspaceAt(workspaceDir, "", true)
	if err != nil {
		return err
	}
	defer ws.Close()
	origin, err := gitOrigin(filepath.Join(workspaceDir, sourceDir))
	if err != nil {
		return fmt.Errorf("schema checkout has no origin remote (%s): %s", sourceDir, shortError(err))
	}
	if origin != options.SourceArg {
		return fmt.Errorf("workspace already initialized from a different source: %s (requested %s)", origin, options.SourceArg)
	}
	if options.SchemaPath != "" && options.SchemaPath != ws.Config.Schema {
		return fmt.Errorf("workspace already uses schema %s; --path %s does not match", ws.Config.Schema, options.SchemaPath)
	}

	fmt.Fprintf(out, "workspace already initialized at %s; finishing setup\n", workspaceDir)
	if options.Harnesses != nil {
		// A rerun with --harness converges the harness list like the rest
		// of the setup; the derived entries follow on the next load.
		ws.Config.Harnesses = options.Harnesses
		if err := saveConfig(workspaceDir, ws.Config); err != nil {
			return err
		}
		ws.Harnesses, ws.HarnessSource = effectiveHarnesses(ws.Config)
		model, err := flattenDefinition(&ws.Def, ws.Harnesses)
		if err != nil {
			return err
		}
		ws.Model = model
		reportHarnesses(out, ws.Config)
	}
	if err := updateSchema(ws, out); err != nil {
		fmt.Fprintf(out, "schema not updated: %s\n", shortError(err))
	}
	if !options.Clone {
		return nil
	}
	if err := cloneIntoWorkspace(ws, options, out); err != nil {
		return err
	}
	return switchAfterInit(ws, options, out)
}

// switchAfterInit runs init's --switch step on the freshly cloned
// repositories: every installed repository goes to the branch, created
// where it does not exist yet, so one command sets up a task workspace.
func switchAfterInit(ws *Workspace, options InitOptions, out io.Writer) error {
	if options.Switch == "" {
		return nil
	}
	return switchWorkspace(ws, SwitchOptions{Branches: []string{options.Switch}, Create: true}, out)
}

// cloneIntoWorkspace runs init's --clone step. State accumulated before a
// clone error is valid and is saved before the error is returned.
func cloneIntoWorkspace(ws *Workspace, options InitOptions, out io.Writer) error {
	cloneErr := clonePathIntoWorkspace(out, ws, CloneOptions{
		Selector:        Selector{Path: options.ClonePath, IncludeArchived: options.IncludeArchived, Tags: options.Tags},
		IncludeOptional: options.IncludeOptional,
		SkipDeps:        options.SkipDeps,
	})
	if err := saveState(ws.Root, ws.State); err != nil {
		return err
	}
	return cloneErr
}

// harnessesOf resolves the harness list a config materializes for.
func harnessesOf(config Config) []string {
	harnesses, _ := effectiveHarnesses(config)
	return harnesses
}

// reportHarnesses prints the one line that makes the harness list visible:
// which harnesses the agents block materializes for and where the list
// came from, so a surprising detection shows up right at init.
func reportHarnesses(out io.Writer, config Config) {
	harnesses, source := effectiveHarnesses(config)
	switch {
	case source == "configured":
		fmt.Fprintf(out, "harnesses: %s (--harness)\n", describeHarnesses(harnesses))
	case len(harnesses) == 0:
		fmt.Fprintln(out, "harnesses: none (nothing detected; use --harness to choose)")
	default:
		fmt.Fprintf(out, "harnesses: %s (detected)\n", describeHarnesses(harnesses))
	}
}

// isGitSource reports whether sourceArg names a git repository rather than a
// local schema file or the bare starter workspace, mirroring setUpSource's
// dispatch: only a successful stat of a regular file means a schema file.
func isGitSource(sourceArg string) bool {
	if sourceArg == "" {
		return false
	}
	info, err := os.Stat(sourceArg)
	return err != nil || info.IsDir()
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
		return seedSource(sourceAbs, sampleSchemaName, []byte(sampleSchema))
	}
	// Only a successful stat of a regular file means a local schema file; any
	// stat error falls through to cloning. Remote URLs are not valid paths on
	// Windows (the colon in user@host:path yields ERROR_INVALID_NAME, not
	// ErrNotExist), and git reports bad sources better than we can.
	if info, err := os.Stat(sourceArg); err == nil && !info.IsDir() {
		if schemaPath != "" {
			return "", errors.New("--path can only be used with Git sources")
		}
		data, err := os.ReadFile(sourceArg)
		if err != nil {
			return "", err
		}
		name := "jig.json"
		if isYAMLSchema(sourceArg) {
			name = "jig.yaml"
		}
		return seedSource(sourceAbs, name, data)
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
// with the given schema content under name (jig.json or jig.yaml).
func seedSource(sourceAbs string, name string, schema []byte) (string, error) {
	if _, err := git("", "init", "--quiet", sourceAbs); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(sourceAbs, name), schema, 0o644); err != nil {
		return "", err
	}
	return name, nil
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
