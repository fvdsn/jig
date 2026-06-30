package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
)

const (
	definitionFile = ".jig.json"
	stateFile      = ".jig/state.json"
)

type Definition struct {
	Version int                        `json:"version"`
	Source  *Source                    `json:"source,omitempty"`
	Tree    map[string]json.RawMessage `json:"tree"`
	Repos   map[string]Repo            `json:"repos,omitempty"` // legacy input is rejected by validation; kept so old files parse clearly.
	Extra   map[string]json.RawMessage `json:"-"`
}

type Source struct {
	Type string `json:"type"`
	URL  string `json:"url"`
	Ref  string `json:"ref,omitempty"`
	Path string `json:"path,omitempty"`
}

type Repo struct {
	ID          string       `json:"id,omitempty"`
	Git         string       `json:"git"`
	Web         string       `json:"web,omitempty"`
	Description string       `json:"description,omitempty"`
	DependsOn   []Dependency `json:"dependsOn,omitempty"`
	OnlyWhen    *Condition   `json:"onlyWhen,omitempty"`
}

type File struct {
	ID          string     `json:"id,omitempty"`
	Src         string     `json:"src"`
	Link        string     `json:"link,omitempty"`
	Description string     `json:"description,omitempty"`
	Executable  bool       `json:"executable,omitempty"`
	OnlyWhen    *Condition `json:"onlyWhen,omitempty"`
}

type Group struct {
	Description string       `json:"description,omitempty"`
	Web         string       `json:"web,omitempty"`
	DependsOn   []Dependency `json:"dependsOn,omitempty"`
	OnlyWhen    *Condition   `json:"onlyWhen,omitempty"`
}

type Dependency struct {
	Path     string `json:"path"`
	Optional bool   `json:"optional,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

type Condition struct {
	Path   string `json:"path"`
	Reason string `json:"reason,omitempty"`
}

type Model struct {
	Repos  map[string]RepoEntry
	Files  map[string]FileEntry
	Groups map[string]GroupEntry
}

type RepoEntry struct {
	Path       string
	Identity   string
	Repo       Repo
	Conditions []Condition
}

type FileEntry struct {
	Path       string
	Identity   string
	File       File
	Conditions []Condition
}

type GroupEntry struct {
	Path       string
	Group      Group
	Conditions []Condition
}

type inheritedGroup struct {
	Description string
	Web         string
	DependsOn   []Dependency
	Conditions  []Condition
}

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

type Workspace struct {
	Root  string
	Def   Definition
	Model Model
	State State
}

type validationResult struct {
	Errors   []string
	Warnings []string
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer, errOut io.Writer) error {
	if len(args) == 0 {
		printUsage(out)
		return nil
	}

	switch args[0] {
	case "init":
		return cmdInit(args[1:], out)
	case "validate":
		return cmdValidate(out)
	case "list":
		return cmdList(out)
	case "info":
		return cmdInfo(args[1:], out)
	case "deps":
		return cmdDeps(args[1:], out)
	case "clone":
		return cmdClone(args[1:], out)
	case "sync":
		return cmdSync(args[1:], out)
	case "pull":
		return cmdPull(args[1:], out)
	case "status":
		return cmdStatus(args[1:], out)
	case "help", "--help", "-h":
		printUsage(out)
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func printUsage(out io.Writer) {
	fmt.Fprintln(out, "Jig is a workspace CLI for managing many related Git repositories and generated files from a shared .jig.json definition.")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Usage:")
	fmt.Fprintln(out, "  jig <command> [args]")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Commands:")
	fmt.Fprintln(out, "  init <git-url-or-file> [workspace-dir] [--path <path>] [--clone [path]] [--with-optional-deps]")
	fmt.Fprintln(out, "      Initialize a workspace from a Git-hosted or local Jig definition, optionally cloning a path.")
	fmt.Fprintln(out, "  validate")
	fmt.Fprintln(out, "      Validate the current workspace .jig.json file.")
	fmt.Fprintln(out, "  list")
	fmt.Fprintln(out, "      List repositories and files defined in .jig.json.")
	fmt.Fprintln(out, "  info <path>")
	fmt.Fprintln(out, "      Show repository, file, or group metadata.")
	fmt.Fprintln(out, "  deps <path>")
	fmt.Fprintln(out, "      Show expanded recursive dependencies for repositories matching a path.")
	fmt.Fprintln(out, "  clone [path] [--with-optional-deps]")
	fmt.Fprintln(out, "      Clone/materialize all entries, or repositories/files matching a path.")
	fmt.Fprintln(out, "  sync [path] [--with-optional-deps]")
	fmt.Fprintln(out, "      Refresh .jig.json from source when configured, then sync local repos/files.")
	fmt.Fprintln(out, "  pull [path]")
	fmt.Fprintln(out, "      Run git pull in installed repositories matching a path or group.")
	fmt.Fprintln(out, "  status [path]")
	fmt.Fprintln(out, "      Show installed, missing, moved, dirty, stale, modified, and remote-changed entries.")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Paths identify repositories, files, or groups using slash paths such as services/checkout or platform.")
}

func cmdInit(args []string, out io.Writer) error {
	parsed, err := parseInitArgs(args)
	if err != nil {
		return err
	}
	if len(parsed.Positionals) == 0 || len(parsed.Positionals) > 2 {
		return errors.New("usage: jig init <git-url-or-file> [workspace-dir] [--path <path>] [--clone [path]] [--with-optional-deps]")
	}

	sourceArg := parsed.Positionals[0]
	workspaceDir := "."
	if len(parsed.Positionals) == 2 {
		workspaceDir = parsed.Positionals[1]
	}
	workspaceDir, err = filepath.Abs(workspaceDir)
	if err != nil {
		return err
	}

	definitionPath := parsed.Values["--path"]
	if definitionPath == "" {
		definitionPath = definitionFile
	}
	if err := validateSafePath(definitionPath); err != nil {
		return fmt.Errorf("invalid definition path: %s", err)
	}

	if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
		return err
	}
	localDefinitionPath := filepath.Join(workspaceDir, definitionFile)
	if _, err := os.Stat(localDefinitionPath); err == nil {
		return errors.New("workspace already initialized: .jig.json exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	def, err := loadInitDefinition(sourceArg, definitionPath)
	if err != nil {
		return err
	}

	validation := validateDefinition(def)
	if len(validation.Errors) > 0 {
		return validation.asError("invalid fetched definition")
	}
	model, err := flattenDefinition(def)
	if err != nil {
		return err
	}

	if err := writeJSON(localDefinitionPath, def); err != nil {
		return err
	}
	if err := saveState(workspaceDir, emptyState()); err != nil {
		return err
	}

	fmt.Fprintf(out, "initialized workspace at %s\n", workspaceDir)
	if parsed.Flags["--clone"] {
		clonePath := parsed.Values["--clone"]
		state, err := loadState(workspaceDir)
		if err != nil {
			return err
		}
		ws := Workspace{Root: workspaceDir, Def: *def, Model: model, State: state}
		if err := clonePathIntoWorkspace(out, &ws, clonePath, parsed.Flags["--with-optional-deps"]); err != nil {
			return err
		}
		if err := saveState(workspaceDir, ws.State); err != nil {
			return err
		}
	}
	return nil
}

func loadInitDefinition(sourceArg string, definitionPath string) (*Definition, error) {
	if info, err := os.Stat(sourceArg); err == nil && !info.IsDir() {
		if definitionPath != definitionFile {
			return nil, errors.New("--path can only be used with Git sources")
		}
		def, err := loadDefinition(sourceArg)
		if err != nil {
			return nil, err
		}
		def.Source = nil
		return def, nil
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	ref, err := discoverDefaultBranch(sourceArg)
	if err != nil {
		return nil, fmt.Errorf("could not determine default branch for %s", sourceArg)
	}
	def, err := fetchDefinition(sourceArg, ref, definitionPath)
	if err != nil {
		return nil, err
	}
	def.Source = &Source{Type: "git", URL: sourceArg, Ref: ref, Path: definitionPath}
	return def, nil
}

func cmdValidate(out io.Writer) error {
	ws, err := loadWorkspace(false)
	if err != nil {
		return err
	}
	validation := validateDefinition(&ws.Def)
	if len(validation.Errors) > 0 {
		for _, msg := range validation.Errors {
			fmt.Fprintf(out, "error: %s\n", msg)
		}
		for _, msg := range validation.Warnings {
			fmt.Fprintf(out, "warning: %s\n", msg)
		}
		return errors.New("validation failed")
	}
	for _, msg := range validation.Warnings {
		fmt.Fprintf(out, "warning: %s\n", msg)
	}
	fmt.Fprintln(out, "valid .jig.json")
	return nil
}

func cmdList(out io.Writer) error {
	ws, err := loadWorkspace(false)
	if err != nil {
		return err
	}
	for _, repoPath := range sortedRepoPaths(&ws.Model) {
		repo := ws.Model.Repos[repoPath].Repo
		fmt.Fprintf(out, "repo  %s", repoPath)
		if repo.Description != "" {
			fmt.Fprintf(out, "\t%s", repo.Description)
		}
		fmt.Fprintln(out)
	}
	for _, filePath := range sortedFilePaths(&ws.Model) {
		file := ws.Model.Files[filePath].File
		fmt.Fprintf(out, "file  %s", filePath)
		if file.Description != "" {
			fmt.Fprintf(out, "\t%s", file.Description)
		}
		fmt.Fprintln(out)
	}
	return nil
}

func cmdInfo(args []string, out io.Writer) error {
	if len(args) != 1 {
		return errors.New("usage: jig info <path>")
	}
	ws, err := loadWorkspace(false)
	if err != nil {
		return err
	}
	path := args[0]
	if err := validateSafePath(path); err != nil {
		return err
	}
	if entry, ok := ws.Model.Repos[path]; ok {
		repo := entry.Repo
		fmt.Fprintf(out, "path: %s\n", path)
		fmt.Fprintln(out, "type: repo")
		fmt.Fprintf(out, "identity: %s\n", entry.Identity)
		fmt.Fprintf(out, "git: %s\n", repo.Git)
		if repo.Web != "" {
			fmt.Fprintf(out, "web: %s\n", repo.Web)
		}
		if repo.Description != "" {
			fmt.Fprintf(out, "description: %s\n", repo.Description)
		}
		if len(entry.Conditions) > 0 {
			printConditions(out, "onlyWhen", entry.Conditions)
		}
		if len(repo.DependsOn) > 0 {
			fmt.Fprintln(out, "dependsOn:")
			for _, dep := range repo.DependsOn {
				optional := ""
				if dep.Optional {
					optional = " optional"
				}
				if dep.Reason == "" {
					fmt.Fprintf(out, "  %s%s\n", dep.Path, optional)
				} else {
					fmt.Fprintf(out, "  %s%s: %s\n", dep.Path, optional, dep.Reason)
				}
			}
		}
		return nil
	}
	if entry, ok := ws.Model.Files[path]; ok {
		file := entry.File
		fmt.Fprintf(out, "path: %s\n", path)
		fmt.Fprintln(out, "type: file")
		fmt.Fprintf(out, "identity: %s\n", entry.Identity)
		if file.Src != "" {
			fmt.Fprintf(out, "src: %s\n", file.Src)
		}
		if file.Link != "" {
			fmt.Fprintf(out, "link: %s\n", file.Link)
		}
		if file.Description != "" {
			fmt.Fprintf(out, "description: %s\n", file.Description)
		}
		fmt.Fprintf(out, "executable: %v\n", file.Executable)
		if len(entry.Conditions) > 0 {
			printConditions(out, "onlyWhen", entry.Conditions)
		}
		return nil
	}

	repos := matchingRepos(&ws.Model, path)
	files := matchingFiles(&ws.Model, path)
	group, hasGroup := ws.Model.Groups[path]
	if len(repos) == 0 && len(files) == 0 && !hasGroup {
		return fmt.Errorf("no repository, file, or group matches %q", path)
	}
	fmt.Fprintf(out, "group: %s\n", path)
	if hasGroup {
		if group.Group.Description != "" {
			fmt.Fprintf(out, "description: %s\n", group.Group.Description)
		}
		if group.Group.Web != "" {
			fmt.Fprintf(out, "web: %s\n", group.Group.Web)
		}
		if len(group.Conditions) > 0 {
			printConditions(out, "onlyWhen", group.Conditions)
		}
		if len(group.Group.DependsOn) > 0 {
			fmt.Fprintln(out, "dependsOn:")
			for _, dep := range group.Group.DependsOn {
				printDependency(out, dep)
			}
		}
	}
	if len(repos) > 0 {
		fmt.Fprintln(out, "repos:")
		for _, repoPath := range repos {
			fmt.Fprintf(out, "  %s\n", repoPath)
		}
	}
	if len(files) > 0 {
		fmt.Fprintln(out, "files:")
		for _, filePath := range files {
			fmt.Fprintf(out, "  %s\n", filePath)
		}
	}
	return nil
}

func printCondition(out io.Writer, label string, condition *Condition) {
	if condition == nil {
		return
	}
	if condition.Reason == "" {
		fmt.Fprintf(out, "%s: %s\n", label, condition.Path)
	} else {
		fmt.Fprintf(out, "%s: %s: %s\n", label, condition.Path, condition.Reason)
	}
}

func printConditions(out io.Writer, label string, conditions []Condition) {
	if len(conditions) == 1 {
		printCondition(out, label, &conditions[0])
		return
	}
	fmt.Fprintf(out, "%s:\n", label)
	for _, condition := range conditions {
		if condition.Reason == "" {
			fmt.Fprintf(out, "  %s\n", condition.Path)
		} else {
			fmt.Fprintf(out, "  %s: %s\n", condition.Path, condition.Reason)
		}
	}
}

func printDependency(out io.Writer, dep Dependency) {
	optional := ""
	if dep.Optional {
		optional = " optional"
	}
	if dep.Reason == "" {
		fmt.Fprintf(out, "  %s%s\n", dep.Path, optional)
	} else {
		fmt.Fprintf(out, "  %s%s: %s\n", dep.Path, optional, dep.Reason)
	}
}

func cmdDeps(args []string, out io.Writer) error {
	parsed, err := parseArgs(args, nil, map[string]bool{"--with-optional-deps": true})
	if err != nil {
		return err
	}
	if len(parsed.Positionals) != 1 {
		return errors.New("usage: jig deps <path> [--with-optional-deps]")
	}
	ws, err := loadWorkspace(false)
	if err != nil {
		return err
	}
	path := parsed.Positionals[0]
	if err := validateSafePath(path); err != nil {
		return err
	}
	roots := matchingRepos(&ws.Model, path)
	if len(roots) == 0 {
		return fmt.Errorf("no repositories match %q", path)
	}
	plan, err := resolvePlan(&ws.Model, roots, planOptions{IncludeOptional: parsed.Flags["--with-optional-deps"], IncludeRoots: false})
	if err != nil {
		return err
	}
	for _, dep := range plan.Repos {
		fmt.Fprintln(out, dep)
	}
	return nil
}

func cmdClone(args []string, out io.Writer) error {
	parsed, err := parseArgs(args, nil, map[string]bool{"--with-optional-deps": true})
	if err != nil {
		return err
	}
	if len(parsed.Positionals) > 1 {
		return errors.New("usage: jig clone [path] [--with-optional-deps]")
	}
	ws, err := loadWorkspace(true)
	if err != nil {
		return err
	}
	path := ""
	if len(parsed.Positionals) == 1 {
		path = parsed.Positionals[0]
	}
	if err := clonePathIntoWorkspace(out, ws, path, parsed.Flags["--with-optional-deps"]); err != nil {
		return err
	}
	return saveState(ws.Root, ws.State)
}

func clonePathIntoWorkspace(out io.Writer, ws *Workspace, path string, includeOptional bool) error {
	roots := sortedRepoPaths(&ws.Model)
	explicitFiles := sortedFilePaths(&ws.Model)
	if path != "" {
		if err := validateSafePath(path); err != nil {
			return err
		}
		roots = matchingRepos(&ws.Model, path)
		explicitFiles = matchingFiles(&ws.Model, path)
	}
	if len(roots) == 0 && len(explicitFiles) == 0 {
		if path == "" {
			return errors.New("no repositories or files defined")
		}
		return fmt.Errorf("no repositories or files match %q", path)
	}
	plan, err := resolvePlan(&ws.Model, roots, planOptions{IncludeOptional: includeOptional, IncludeRoots: true, Installed: installedRepoIdentitySet(ws.Root, &ws.Model, &ws.State)})
	if err != nil {
		return err
	}
	plan = includeExplicitFiles(&ws.Model, plan, explicitFiles)
	applyPlan(out, ws, plan, false)
	return nil
}

func cmdSync(args []string, out io.Writer) error {
	parsed, err := parseArgs(args, nil, map[string]bool{"--with-optional-deps": true})
	if err != nil {
		return err
	}
	if len(parsed.Positionals) > 1 {
		return errors.New("usage: jig sync [path] [--with-optional-deps]")
	}
	ws, err := loadWorkspace(true)
	if err != nil {
		return err
	}
	if err := refreshWorkspaceDefinition(out, ws); err != nil {
		return err
	}

	path := ""
	if len(parsed.Positionals) == 1 {
		path = parsed.Positionals[0]
	}
	return syncWorkspace(out, ws, path, parsed.Flags["--with-optional-deps"])
}

func refreshWorkspaceDefinition(out io.Writer, ws *Workspace) error {
	if ws.Def.Source == nil {
		return nil
	}
	source := *ws.Def.Source
	if source.Type != "git" {
		return fmt.Errorf("unsupported source type %q", source.Type)
	}
	if source.Path == "" {
		source.Path = definitionFile
	}
	if err := validateSafePath(source.Path); err != nil {
		return fmt.Errorf("invalid source path: %s", err)
	}
	if source.Ref == "" {
		ref, err := discoverDefaultBranch(source.URL)
		if err != nil {
			return fmt.Errorf("could not determine default branch for %s", source.URL)
		}
		source.Ref = ref
	}
	incoming, err := fetchDefinition(source.URL, source.Ref, source.Path)
	if err != nil {
		return err
	}
	incoming.Source = &source
	validation := validateDefinition(incoming)
	if len(validation.Errors) > 0 {
		return validation.asError("invalid incoming definition")
	}
	incomingModel, err := flattenDefinition(incoming)
	if err != nil {
		return err
	}
	printDefinitionChanges(out, &ws.Model, &incomingModel)
	if err := writeJSON(filepath.Join(ws.Root, definitionFile), incoming); err != nil {
		return err
	}
	ws.Def = *incoming
	ws.Model = incomingModel
	return nil
}

func syncWorkspace(out io.Writer, ws *Workspace, path string, includeOptional bool) error {
	var roots []string
	var explicitFiles []string
	if path != "" {
		if err := validateSafePath(path); err != nil {
			return err
		}
		roots = matchingRepos(&ws.Model, path)
		explicitFiles = matchingFiles(&ws.Model, path)
		if len(roots) == 0 && len(explicitFiles) == 0 {
			return fmt.Errorf("no repositories or files match %q", path)
		}
	} else {
		roots = installedDefinedRepos(ws.Root, &ws.Model, &ws.State)
	}

	plan, err := resolvePlan(&ws.Model, roots, planOptions{IncludeOptional: includeOptional, IncludeInstalledOptional: true, IncludeRoots: true, Installed: installedRepoIdentitySet(ws.Root, &ws.Model, &ws.State)})
	if err != nil {
		return err
	}
	plan = includeExplicitFiles(&ws.Model, plan, explicitFiles)
	applyPlan(out, ws, plan, true)
	reportStale(out, ws.Root, &ws.Model, &ws.State)
	return saveState(ws.Root, ws.State)
}

func includeExplicitFiles(model *Model, base plan, files []string) plan {
	active := map[string]bool{}
	for _, filePath := range base.Files {
		active[filePath] = true
	}
	var add func(string)
	add = func(filePath string) {
		entry, ok := model.Files[filePath]
		if !ok {
			return
		}
		if entry.File.Link != "" {
			add(entry.File.Link)
		}
		active[filePath] = true
	}
	for _, filePath := range files {
		add(filePath)
	}
	base.Files = orderFilesForApply(model, active)
	return base
}

func applyPlan(out io.Writer, ws *Workspace, plan plan, allowMove bool) {
	for _, repoPath := range plan.Repos {
		if err := ensureRepo(out, ws.Root, &ws.Model, &ws.State, repoPath, allowMove); err != nil {
			fmt.Fprintf(out, "skipped:\n  %s: %s\n", repoPath, err)
		}
	}
	for _, filePath := range plan.Files {
		if err := ensureFile(out, ws.Root, &ws.Model, &ws.State, filePath, allowMove); err != nil {
			fmt.Fprintf(out, "skipped:\n  %s: %s\n", filePath, err)
		}
	}
}

func cmdPull(args []string, out io.Writer) error {
	if len(args) > 1 {
		return errors.New("usage: jig pull [path]")
	}
	ws, err := loadWorkspace(false)
	if err != nil {
		return err
	}
	filter := ""
	if len(args) == 1 {
		filter = args[0]
		if err := validateSafePath(filter); err != nil {
			return err
		}
	}

	var pulled []string
	var skipped []string
	for _, repoPath := range sortedRepoPaths(&ws.Model) {
		if filter != "" && !pathMatches(filter, repoPath) {
			continue
		}
		local, ok := installedPath(ws.Root, &ws.Model, &ws.State, repoPath)
		if !ok {
			continue
		}
		if _, err := git(local, "pull"); err != nil {
			skipped = append(skipped, fmt.Sprintf("%s: %s", repoPath, shortError(err)))
			continue
		}
		pulled = append(pulled, repoPath)
	}
	printGroup(out, "pulled", pulled)
	printGroup(out, "skipped", skipped)
	return nil
}

func cmdStatus(args []string, out io.Writer) error {
	if len(args) > 1 {
		return errors.New("usage: jig status [path]")
	}
	ws, err := loadWorkspace(false)
	if err != nil {
		return err
	}
	filter := ""
	if len(args) == 1 {
		filter = args[0]
		if err := validateSafePath(filter); err != nil {
			return err
		}
	}

	installedIDs := installedRepoIdentitySet(ws.Root, &ws.Model, &ws.State)
	activeFiles := activeFiles(&ws.Model, installedIDs)

	var installed []string
	var missing []string
	var moved []string
	var remoteChanged []string
	var dirty []string
	var written []string
	var modified []string
	var conflicts []string

	for _, repoPath := range sortedRepoPaths(&ws.Model) {
		if filter != "" && !pathMatches(filter, repoPath) {
			continue
		}
		entry := ws.Model.Repos[repoPath]
		expectedAbs := filepath.Join(ws.Root, entry.Path)
		stateRepo, hasState := ws.State.Repos[entry.Identity]
		if hasState && stateRepo.Path != entry.Path && isGitRepo(filepath.Join(ws.Root, stateRepo.Path)) {
			moved = append(moved, fmt.Sprintf("%s: %s -> %s", repoPath, stateRepo.Path, entry.Path))
			if isDirty(filepath.Join(ws.Root, stateRepo.Path)) {
				dirty = append(dirty, repoPath)
			}
			continue
		}
		if !pathExists(expectedAbs) {
			missing = append(missing, repoPath)
			continue
		}
		if !isGitRepo(expectedAbs) {
			conflicts = append(conflicts, fmt.Sprintf("%s: expected path is not a Git repository", repoPath))
			continue
		}
		origin, err := gitOrigin(expectedAbs)
		if err != nil || origin != entry.Repo.Git {
			remoteChanged = append(remoteChanged, fmt.Sprintf("%s: %s -> %s", repoPath, origin, entry.Repo.Git))
		}
		if isDirty(expectedAbs) {
			dirty = append(dirty, repoPath)
		}
		installed = append(installed, repoPath)
	}

	for _, filePath := range sortedFilePaths(&ws.Model) {
		if filter != "" && !pathMatches(filter, filePath) {
			continue
		}
		if !activeFiles[filePath] {
			continue
		}
		entry := ws.Model.Files[filePath]
		stateFile, hasState := ws.State.Files[entry.Identity]
		expectedAbs := filepath.Join(ws.Root, entry.Path)
		if hasState && stateFile.Path != entry.Path && pathExists(filepath.Join(ws.Root, stateFile.Path)) {
			moved = append(moved, fmt.Sprintf("%s: %s -> %s", filePath, stateFile.Path, entry.Path))
			continue
		}
		if !pathExists(expectedAbs) {
			missing = append(missing, filePath)
			continue
		}
		if !hasState {
			conflicts = append(conflicts, fmt.Sprintf("%s: existing file is not tracked", filePath))
			continue
		}
		if entry.File.Link != "" {
			info, err := os.Lstat(expectedAbs)
			if err != nil {
				conflicts = append(conflicts, fmt.Sprintf("%s: %s", filePath, err))
				continue
			}
			if info.Mode()&os.ModeSymlink == 0 {
				conflicts = append(conflicts, fmt.Sprintf("%s: expected symlink path is not a symlink", filePath))
				continue
			}
			targetEntry := ws.Model.Files[entry.File.Link]
			expectedTarget, err := relativeSymlinkTarget(entry.Path, targetEntry.Path)
			if err != nil {
				conflicts = append(conflicts, fmt.Sprintf("%s: %s", filePath, err))
				continue
			}
			currentTarget, err := os.Readlink(expectedAbs)
			if err != nil {
				conflicts = append(conflicts, fmt.Sprintf("%s: %s", filePath, err))
				continue
			}
			if currentTarget != expectedTarget {
				modified = append(modified, filePath)
			} else {
				written = append(written, filePath)
			}
			continue
		}
		currentHash, err := fileSHA256(expectedAbs)
		if err != nil {
			conflicts = append(conflicts, fmt.Sprintf("%s: %s", filePath, err))
			continue
		}
		if currentHash != stateFile.SHA256 {
			modified = append(modified, filePath)
		} else {
			written = append(written, filePath)
		}
	}

	var stale []string
	if filter == "" {
		for identity, stateRepo := range ws.State.Repos {
			if _, ok := repoIdentityToPath(&ws.Model)[identity]; !ok {
				stale = append(stale, fmt.Sprintf("%s at %s is no longer defined", identity, stateRepo.Path))
			}
		}
		for identity, stateFile := range ws.State.Files {
			if _, ok := fileIdentityToPath(&ws.Model)[identity]; !ok {
				stale = append(stale, fmt.Sprintf("%s at %s is no longer defined", identity, stateFile.Path))
			}
		}
	}

	printGroup(out, "installed", installed)
	printGroup(out, "written", written)
	printGroup(out, "moved", moved)
	printGroup(out, "missing", missing)
	printGroup(out, "remote-changed", remoteChanged)
	printGroup(out, "dirty", dirty)
	printGroup(out, "modified", modified)
	printGroup(out, "conflicts", conflicts)
	printGroup(out, "stale", stale)
	return nil
}

func validateDefinition(def *Definition) validationResult {
	var result validationResult
	if def.Version != 1 {
		result.Errors = append(result.Errors, "unsupported or missing version")
	}
	if def.Tree == nil {
		result.Errors = append(result.Errors, "missing tree")
	}
	if len(def.Repos) > 0 {
		result.Errors = append(result.Errors, "legacy repos field is not supported; use tree with $repo nodes")
	}
	if def.Source != nil {
		if def.Source.Type != "git" {
			result.Errors = append(result.Errors, "source.type must be git")
		}
		if def.Source.URL == "" {
			result.Errors = append(result.Errors, "source.url is required")
		}
		if def.Source.Path != "" {
			if err := validateSafePath(def.Source.Path); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("invalid source.path: %s", err))
			}
		}
	}

	model, err := flattenDefinition(def)
	if err != nil {
		result.Errors = append(result.Errors, err.Error())
		return result
	}

	repoIDs := map[string]string{}
	for _, path := range sortedRepoPaths(&model) {
		entry := model.Repos[path]
		if entry.Repo.Git == "" {
			result.Errors = append(result.Errors, fmt.Sprintf("repository %s missing git", path))
		}
		if prev, ok := repoIDs[entry.Identity]; ok {
			result.Errors = append(result.Errors, fmt.Sprintf("duplicate repository identity %s: %s and %s", entry.Identity, prev, path))
		} else {
			repoIDs[entry.Identity] = path
		}
		for _, condition := range entry.Conditions {
			condition := condition
			validateCondition(&result, model, path, &condition)
		}
		for _, dep := range entry.Repo.DependsOn {
			if err := validateSafePath(dep.Path); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("repository %s has invalid dependency path %q: %s", path, dep.Path, err))
				continue
			}
			if len(matchingRepos(&model, dep.Path)) == 0 {
				result.Errors = append(result.Errors, fmt.Sprintf("repository %s dependency %s does not resolve to any repository", path, dep.Path))
			}
		}
	}

	fileIDs := map[string]string{}
	for _, path := range sortedFilePaths(&model) {
		entry := model.Files[path]
		if (entry.File.Src == "") == (entry.File.Link == "") {
			result.Errors = append(result.Errors, fmt.Sprintf("file %s must define exactly one of src or link", path))
		}
		if entry.File.Src != "" {
			if _, err := parseFileSrc(entry.File.Src); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("file %s invalid src: %s", path, err))
			}
		}
		if entry.File.Link != "" {
			if err := validateSafePath(entry.File.Link); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("file %s invalid link: %s", path, err))
			} else if _, ok := model.Files[entry.File.Link]; !ok {
				result.Errors = append(result.Errors, fmt.Sprintf("file %s link %s does not resolve to any file", path, entry.File.Link))
			} else if entry.File.Link == path {
				result.Errors = append(result.Errors, fmt.Sprintf("file %s cannot link to itself", path))
			}
		}
		if entry.File.Executable && entry.File.Link != "" {
			result.Errors = append(result.Errors, fmt.Sprintf("file %s cannot use executable with link", path))
		}
		if prev, ok := fileIDs[entry.Identity]; ok {
			result.Errors = append(result.Errors, fmt.Sprintf("duplicate file identity %s: %s and %s", entry.Identity, prev, path))
		} else {
			fileIDs[entry.Identity] = path
		}
		for _, condition := range entry.Conditions {
			condition := condition
			validateCondition(&result, model, path, &condition)
		}
	}

	for path, entry := range model.Groups {
		for _, dep := range entry.Group.DependsOn {
			if err := validateSafePath(dep.Path); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("group %s has invalid dependency path %q: %s", path, dep.Path, err))
				continue
			}
			if len(matchingRepos(&model, dep.Path)) == 0 {
				result.Errors = append(result.Errors, fmt.Sprintf("group %s dependency %s does not resolve to any repository", path, dep.Path))
			}
		}
		for _, condition := range entry.Conditions {
			condition := condition
			validateCondition(&result, model, path, &condition)
		}
	}

	for _, cycle := range detectCycles(&model) {
		result.Warnings = append(result.Warnings, "dependency cycle detected: "+strings.Join(cycle, " -> "))
	}
	for _, cycle := range detectFileLinkCycles(&model) {
		result.Errors = append(result.Errors, "file link cycle detected: "+strings.Join(cycle, " -> "))
	}
	return result
}

func detectFileLinkCycles(model *Model) [][]string {
	visited := map[string]int{}
	var stack []string
	var cycles [][]string
	seen := map[string]bool{}
	var visit func(string)
	visit = func(path string) {
		if visited[path] == 2 {
			return
		}
		if visited[path] == 1 {
			idx := indexOf(stack, path)
			if idx >= 0 {
				cycle := append([]string{}, stack[idx:]...)
				cycle = append(cycle, path)
				key := strings.Join(cycle, "\x00")
				if !seen[key] {
					cycles = append(cycles, cycle)
					seen[key] = true
				}
			}
			return
		}
		visited[path] = 1
		stack = append(stack, path)
		if entry, ok := model.Files[path]; ok && entry.File.Link != "" {
			visit(entry.File.Link)
		}
		stack = stack[:len(stack)-1]
		visited[path] = 2
	}
	for _, path := range sortedFilePaths(model) {
		visit(path)
	}
	return cycles
}

func validateCondition(result *validationResult, model Model, ownerPath string, condition *Condition) {
	if condition.Path == "" {
		result.Errors = append(result.Errors, fmt.Sprintf("%s has onlyWhen with empty path", ownerPath))
		return
	}
	if err := validateSafePath(condition.Path); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("%s has invalid onlyWhen path %q: %s", ownerPath, condition.Path, err))
		return
	}
	if len(matchingRepos(&model, condition.Path)) == 0 {
		result.Errors = append(result.Errors, fmt.Sprintf("%s onlyWhen path %s does not resolve to any repository", ownerPath, condition.Path))
	}
}

func (v validationResult) asError(prefix string) error {
	var b strings.Builder
	b.WriteString(prefix)
	for _, msg := range v.Errors {
		b.WriteString("\n  ")
		b.WriteString(msg)
	}
	return errors.New(b.String())
}

func flattenDefinition(def *Definition) (Model, error) {
	model := Model{Repos: map[string]RepoEntry{}, Files: map[string]FileEntry{}, Groups: map[string]GroupEntry{}}
	if def.Tree == nil {
		return model, errors.New("missing tree")
	}
	if err := flattenTreeMap(def.Tree, "", inheritedGroup{}, &model); err != nil {
		return model, err
	}
	return model, nil
}

func flattenTreeMap(nodes map[string]json.RawMessage, prefix string, inherited inheritedGroup, model *Model) error {
	keys := make([]string, 0, len(nodes))
	for key := range nodes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if strings.HasPrefix(key, "$") {
			return fmt.Errorf("reserved tree key %q cannot be used as a path segment", key)
		}
		path, err := joinTreePath(prefix, key)
		if err != nil {
			return err
		}
		if err := flattenTreeNode(path, nodes[key], inherited, model); err != nil {
			return err
		}
	}
	return nil
}

func flattenTreeNode(path string, raw json.RawMessage, inherited inheritedGroup, model *Model) error {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return fmt.Errorf("invalid tree node %s: %s", path, err)
	}
	_, hasRepo := obj["$repo"]
	_, hasFile := obj["$file"]
	groupRaw, hasGroup := obj["$group"]
	if hasRepo && hasFile {
		return fmt.Errorf("tree node %s cannot contain both $repo and $file", path)
	}
	if hasRepo || hasFile {
		if len(obj) != 1 {
			return fmt.Errorf("tree node %s cannot contain child nodes with $repo or $file", path)
		}
		if err := validateSafePath(path); err != nil {
			return fmt.Errorf("invalid tree path %q: %s", path, err)
		}
		if hasRepo {
			var repo Repo
			if err := json.Unmarshal(obj["$repo"], &repo); err != nil {
				return fmt.Errorf("invalid $repo at %s: %s", path, err)
			}
			identity := repo.ID
			if identity == "" {
				identity = path
			}
			repo = applyInheritedRepo(repo, inherited)
			conditions := append([]Condition{}, inherited.Conditions...)
			if repo.OnlyWhen != nil {
				conditions = append(conditions, *repo.OnlyWhen)
			}
			model.Repos[path] = RepoEntry{Path: path, Identity: identity, Repo: repo, Conditions: conditions}
			return nil
		}
		var file File
		if err := json.Unmarshal(obj["$file"], &file); err != nil {
			return fmt.Errorf("invalid $file at %s: %s", path, err)
		}
		identity := file.ID
		if identity == "" {
			identity = path
		}
		file = applyInheritedFile(file, inherited)
		conditions := append([]Condition{}, inherited.Conditions...)
		if file.OnlyWhen != nil {
			conditions = append(conditions, *file.OnlyWhen)
		}
		model.Files[path] = FileEntry{Path: path, Identity: identity, File: file, Conditions: conditions}
		return nil
	}
	if hasGroup {
		var group Group
		if err := json.Unmarshal(groupRaw, &group); err != nil {
			return fmt.Errorf("invalid $group at %s: %s", path, err)
		}
		delete(obj, "$group")
		inherited = mergeGroup(inherited, group)
		model.Groups[path] = GroupEntry{Path: path, Group: group, Conditions: append([]Condition{}, inherited.Conditions...)}
	}
	return flattenTreeMap(obj, path, inherited, model)
}

func applyInheritedRepo(repo Repo, inherited inheritedGroup) Repo {
	if repo.Description == "" {
		repo.Description = inherited.Description
	}
	if repo.Web == "" {
		repo.Web = inherited.Web
	}
	if len(inherited.DependsOn) > 0 {
		deps := append([]Dependency{}, inherited.DependsOn...)
		repo.DependsOn = append(deps, repo.DependsOn...)
	}
	return repo
}

func applyInheritedFile(file File, inherited inheritedGroup) File {
	if file.Description == "" {
		file.Description = inherited.Description
	}
	return file
}

func mergeGroup(inherited inheritedGroup, group Group) inheritedGroup {
	merged := inheritedGroup{
		Description: inherited.Description,
		Web:         inherited.Web,
		DependsOn:   append([]Dependency{}, inherited.DependsOn...),
		Conditions:  append([]Condition{}, inherited.Conditions...),
	}
	if group.Description != "" {
		merged.Description = group.Description
	}
	if group.Web != "" {
		merged.Web = group.Web
	}
	if len(group.DependsOn) > 0 {
		merged.DependsOn = append(merged.DependsOn, group.DependsOn...)
	}
	if group.OnlyWhen != nil {
		merged.Conditions = append(merged.Conditions, *group.OnlyWhen)
	}
	return merged
}

func joinTreePath(prefix, key string) (string, error) {
	if err := validateSafePath(key); err != nil {
		return "", fmt.Errorf("invalid tree key %q: %s", key, err)
	}
	if prefix == "" {
		return key, nil
	}
	return prefix + "/" + key, nil
}

func validateSafePath(path string) error {
	if path == "" {
		return errors.New("path is empty")
	}
	if filepath.IsAbs(path) || strings.HasPrefix(path, "/") {
		return errors.New("path must be relative")
	}
	if strings.HasPrefix(path, "~") {
		return errors.New("path must not start with ~")
	}
	if strings.Contains(path, "\\") {
		return errors.New("path must use / separators")
	}
	segments := strings.Split(path, "/")
	for _, segment := range segments {
		if segment == "" {
			return errors.New("path must not contain empty segments")
		}
		if segment == "." || segment == ".." {
			return errors.New("path must not contain . or .. segments")
		}
	}
	return nil
}

type fileSrc struct {
	GitURL string
	Path   string
}

func parseFileSrc(src string) (fileSrc, error) {
	if !strings.HasPrefix(src, "git:") {
		return fileSrc{}, errors.New("src must start with git:")
	}
	value := strings.TrimPrefix(src, "git:")
	idx := strings.LastIndex(value, "#")
	if idx <= 0 || idx == len(value)-1 {
		return fileSrc{}, errors.New("src must be git:<repo-url>#<file-path>")
	}
	parsed := fileSrc{GitURL: value[:idx], Path: value[idx+1:]}
	if strings.Contains(parsed.Path, "#") {
		return fileSrc{}, errors.New("source file path must not contain #")
	}
	if err := validateSafePath(parsed.Path); err != nil {
		return fileSrc{}, err
	}
	return parsed, nil
}

func detectCycles(model *Model) [][]string {
	visited := map[string]int{}
	var stack []string
	var cycles [][]string
	seen := map[string]bool{}

	var visit func(string)
	visit = func(repoPath string) {
		if visited[repoPath] == 2 {
			return
		}
		if visited[repoPath] == 1 {
			idx := indexOf(stack, repoPath)
			if idx >= 0 {
				cycle := append([]string{}, stack[idx:]...)
				cycle = append(cycle, repoPath)
				key := strings.Join(cycle, "\x00")
				if !seen[key] {
					cycles = append(cycles, cycle)
					seen[key] = true
				}
			}
			return
		}
		visited[repoPath] = 1
		stack = append(stack, repoPath)
		for _, dep := range model.Repos[repoPath].Repo.DependsOn {
			for _, match := range matchingRepos(model, dep.Path) {
				visit(match)
			}
		}
		stack = stack[:len(stack)-1]
		visited[repoPath] = 2
	}

	for _, repoPath := range sortedRepoPaths(model) {
		visit(repoPath)
	}
	return cycles
}

func indexOf(items []string, value string) int {
	for i, item := range items {
		if item == value {
			return i
		}
	}
	return -1
}

type planOptions struct {
	IncludeOptional          bool
	IncludeInstalledOptional bool
	IncludeRoots             bool
	Installed                map[string]bool
}

type plan struct {
	Repos []string
	Files []string
}

func resolvePlan(model *Model, roots []string, opts planOptions) (plan, error) {
	if opts.Installed == nil {
		opts.Installed = map[string]bool{}
	}
	active := map[string]bool{}
	rootIDs := map[string]bool{}
	for _, root := range roots {
		entry, ok := model.Repos[root]
		if !ok {
			return plan{}, fmt.Errorf("unknown repository %q", root)
		}
		rootIDs[entry.Identity] = true
		if opts.IncludeRoots {
			active[root] = true
		}
	}

	changed := true
	for changed {
		changed = false
		for _, root := range roots {
			if !opts.IncludeRoots {
				if changedRoot, err := addDependencies(model, root, active, opts, rootIDs); err != nil {
					return plan{}, err
				} else if changedRoot {
					changed = true
				}
			}
		}
		for _, repoPath := range sortedRepoPaths(model) {
			entry := model.Repos[repoPath]
			if active[repoPath] {
				if changedDeps, err := addDependencies(model, repoPath, active, opts, rootIDs); err != nil {
					return plan{}, err
				} else if changedDeps {
					changed = true
				}
				continue
			}
			if len(entry.Conditions) > 0 && conditionsMatch(entry.Conditions, active, opts.Installed, model) {
				active[repoPath] = true
				changed = true
			}
		}
	}

	activeFilesSet := activeFilesForRepoSet(model, active, opts.Installed)
	return plan{Repos: sortedKeys(active), Files: orderFilesForApply(model, activeFilesSet)}, nil
}

func orderFilesForApply(model *Model, active map[string]bool) []string {
	visited := map[string]bool{}
	visiting := map[string]bool{}
	var result []string
	var visit func(string)
	visit = func(path string) {
		if visited[path] || !active[path] {
			return
		}
		if visiting[path] {
			return
		}
		visiting[path] = true
		if entry, ok := model.Files[path]; ok && entry.File.Link != "" {
			visit(entry.File.Link)
		}
		visiting[path] = false
		visited[path] = true
		result = append(result, path)
	}
	for _, path := range sortedKeys(active) {
		visit(path)
	}
	return result
}

func addDependencies(model *Model, repoPath string, active map[string]bool, opts planOptions, excludedIDs map[string]bool) (bool, error) {
	entry := model.Repos[repoPath]
	changed := false
	for _, dep := range entry.Repo.DependsOn {
		matches := matchingRepos(model, dep.Path)
		if len(matches) == 0 {
			return false, fmt.Errorf("dependency %s for %s does not resolve to any repository", dep.Path, repoPath)
		}
		for _, match := range matches {
			matchEntry := model.Repos[match]
			if dep.Optional && !opts.IncludeOptional && !(opts.IncludeInstalledOptional && opts.Installed[matchEntry.Identity]) {
				continue
			}
			if excludedIDs[matchEntry.Identity] {
				continue
			}
			if len(matchEntry.Conditions) > 0 && !conditionsMatch(matchEntry.Conditions, active, opts.Installed, model) {
				continue
			}
			if !active[match] {
				active[match] = true
				changed = true
			}
		}
	}
	return changed, nil
}

func activeFiles(model *Model, installed map[string]bool) map[string]bool {
	return activeFilesForRepoSet(model, map[string]bool{}, installed)
}

func activeFilesForRepoSet(model *Model, activeRepos map[string]bool, installed map[string]bool) map[string]bool {
	files := map[string]bool{}
	changed := true
	for changed {
		changed = false
		for _, filePath := range sortedFilePaths(model) {
			if files[filePath] {
				continue
			}
			entry := model.Files[filePath]
			if len(entry.Conditions) > 0 && !conditionsMatch(entry.Conditions, activeRepos, installed, model) {
				continue
			}
			if entry.File.Link != "" && !files[entry.File.Link] {
				continue
			}
			files[filePath] = true
			changed = true
		}
	}
	return files
}

func conditionsMatch(conditions []Condition, activeRepos map[string]bool, installed map[string]bool, model *Model) bool {
	for _, condition := range conditions {
		condition := condition
		if !conditionMatches(&condition, activeRepos, installed, model) {
			return false
		}
	}
	return true
}

func conditionMatches(condition *Condition, activeRepos map[string]bool, installed map[string]bool, model *Model) bool {
	if condition == nil {
		return true
	}
	for repoPath := range activeRepos {
		if pathMatches(condition.Path, repoPath) {
			return true
		}
	}
	for identity := range installed {
		if repoPath, ok := repoIdentityToPath(model)[identity]; ok && pathMatches(condition.Path, repoPath) {
			return true
		}
	}
	return false
}

func matchingRepos(model *Model, path string) []string {
	var matches []string
	for _, repoPath := range sortedRepoPaths(model) {
		if pathMatches(path, repoPath) {
			matches = append(matches, repoPath)
		}
	}
	return matches
}

func matchingFiles(model *Model, path string) []string {
	var matches []string
	for _, filePath := range sortedFilePaths(model) {
		if pathMatches(path, filePath) {
			matches = append(matches, filePath)
		}
	}
	return matches
}

func pathMatches(path string, entryPath string) bool {
	return entryPath == path || strings.HasPrefix(entryPath, path+"/")
}

func sortedRepoPaths(model *Model) []string {
	paths := make([]string, 0, len(model.Repos))
	for path := range model.Repos {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func sortedFilePaths(model *Model) []string {
	paths := make([]string, 0, len(model.Files))
	for path := range model.Files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func sortedKeys(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func repoIdentityToPath(model *Model) map[string]string {
	result := map[string]string{}
	for path, entry := range model.Repos {
		result[entry.Identity] = path
	}
	return result
}

func fileIdentityToPath(model *Model) map[string]string {
	result := map[string]string{}
	for path, entry := range model.Files {
		result[entry.Identity] = path
	}
	return result
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

func loadDefinition(path string) (*Definition, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var def Definition
	if err := json.Unmarshal(data, &def); err != nil {
		return nil, err
	}
	return &def, nil
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

func discoverDefaultBranch(gitURL string) (string, error) {
	out, err := exec.Command("git", "ls-remote", "--symref", gitURL, "HEAD").Output()
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "ref: refs/heads/") {
			continue
		}
		line = strings.TrimPrefix(line, "ref: refs/heads/")
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		return fields[0], nil
	}
	return "", errors.New("default branch not found")
}

func fetchDefinition(gitURL, ref, definitionPath string) (*Definition, error) {
	data, err := fetchGitFile(gitURL, ref, definitionPath)
	if err != nil {
		return nil, err
	}
	var def Definition
	if err := json.Unmarshal(data, &def); err != nil {
		return nil, err
	}
	return &def, nil
}

func fetchGitFile(gitURL, ref, sourcePath string) ([]byte, error) {
	if sourcePath == "" {
		sourcePath = definitionFile
	}
	if err := validateSafePath(sourcePath); err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp("", "jig-source-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	repoDir := filepath.Join(tmp, "repo")
	args := []string{"clone", "--quiet", "--depth", "1"}
	if ref != "" {
		args = append(args, "--branch", ref, "--single-branch")
	}
	args = append(args, gitURL, repoDir)
	if _, err := git("", args...); err != nil {
		return nil, err
	}
	return os.ReadFile(filepath.Join(repoDir, sourcePath))
}

func ensureRepo(out io.Writer, root string, model *Model, state *State, repoPath string, allowMove bool) error {
	entry := model.Repos[repoPath]
	repo := entry.Repo
	expectedRel := entry.Path
	expectedAbs := filepath.Join(root, expectedRel)
	stateRepo, hasState := state.Repos[entry.Identity]

	if hasState && stateRepo.Path != expectedRel {
		oldAbs := filepath.Join(root, stateRepo.Path)
		if isGitRepo(oldAbs) {
			if !allowMove {
				return fmt.Errorf("already installed at %s; run jig sync to move it", stateRepo.Path)
			}
			if pathExists(expectedAbs) {
				return fmt.Errorf("target path already exists: %s", expectedRel)
			}
			if isDirty(oldAbs) {
				return fmt.Errorf("repository has uncommitted changes and would need to be moved")
			}
			if err := os.MkdirAll(filepath.Dir(expectedAbs), 0o755); err != nil {
				return err
			}
			if err := os.Rename(oldAbs, expectedAbs); err != nil {
				return err
			}
			pruneEmptyParents(root, filepath.Dir(stateRepo.Path))
			fmt.Fprintf(out, "moved: %s: %s -> %s\n", repoPath, stateRepo.Path, expectedRel)
			stateRepo.Path = expectedRel
			state.Repos[entry.Identity] = stateRepo
			hasState = true
		} else {
			delete(state.Repos, entry.Identity)
			hasState = false
		}
	}

	if !pathExists(expectedAbs) {
		if err := os.MkdirAll(filepath.Dir(expectedAbs), 0o755); err != nil {
			return err
		}
		if _, err := git("", "clone", repo.Git, expectedAbs); err != nil {
			return err
		}
		state.Repos[entry.Identity] = StateRepo{Path: expectedRel, Git: repo.Git}
		fmt.Fprintf(out, "cloned: %s\n", repoPath)
		return nil
	}

	if !isGitRepo(expectedAbs) {
		return fmt.Errorf("expected path exists and is not a Git repository: %s", expectedRel)
	}
	origin, err := gitOrigin(expectedAbs)
	if err != nil {
		return err
	}
	if origin != repo.Git {
		if allowMove && hasState && state.Repos[entry.Identity].Path == expectedRel {
			if _, err := git(expectedAbs, "remote", "set-url", "origin", repo.Git); err != nil {
				return err
			}
			state.Repos[entry.Identity] = StateRepo{Path: expectedRel, Git: repo.Git}
			fmt.Fprintf(out, "updated-origin: %s\n", repoPath)
			return nil
		}
		return fmt.Errorf("existing Git repository has different origin at %s", expectedRel)
	}
	state.Repos[entry.Identity] = StateRepo{Path: expectedRel, Git: repo.Git}
	fmt.Fprintf(out, "present: %s\n", repoPath)
	return nil
}

func ensureFile(out io.Writer, root string, model *Model, state *State, filePath string, allowMove bool) error {
	entry := model.Files[filePath]
	file := entry.File
	if file.Link != "" {
		return ensureLinkFile(out, root, model, state, filePath, allowMove)
	}
	stateFile, hasState := state.Files[entry.Identity]
	expectedRel := entry.Path
	expectedAbs := filepath.Join(root, expectedRel)

	if hasState && stateFile.Path != expectedRel {
		oldAbs := filepath.Join(root, stateFile.Path)
		if pathExists(oldAbs) {
			currentHash, err := fileSHA256(oldAbs)
			if err != nil {
				return err
			}
			if currentHash != stateFile.SHA256 {
				return fmt.Errorf("locally modified")
			}
			if !allowMove {
				return fmt.Errorf("already written at %s; run jig sync to move it", stateFile.Path)
			}
			if pathExists(expectedAbs) {
				return fmt.Errorf("target path already exists: %s", expectedRel)
			}
			if err := os.MkdirAll(filepath.Dir(expectedAbs), 0o755); err != nil {
				return err
			}
			if err := os.Rename(oldAbs, expectedAbs); err != nil {
				return err
			}
			pruneEmptyParents(root, filepath.Dir(stateFile.Path))
			fmt.Fprintf(out, "moved-file: %s: %s -> %s\n", filePath, stateFile.Path, expectedRel)
			stateFile.Path = expectedRel
			state.Files[entry.Identity] = stateFile
			hasState = true
		} else {
			delete(state.Files, entry.Identity)
			hasState = false
		}
	}

	if pathExists(expectedAbs) {
		info, err := os.Stat(expectedAbs)
		if err != nil {
			return err
		}
		if info.IsDir() {
			return fmt.Errorf("expected file path is a directory: %s", expectedRel)
		}
		if !hasState {
			return fmt.Errorf("existing file is not tracked")
		}
		currentHash, err := fileSHA256(expectedAbs)
		if err != nil {
			return err
		}
		if currentHash != stateFile.SHA256 {
			return fmt.Errorf("locally modified")
		}
	} else if hasState {
		delete(state.Files, entry.Identity)
	}

	content, err := fetchFileContent(file.Src)
	if err != nil {
		return err
	}
	newHash := sha256Hex(content)

	if err := os.MkdirAll(filepath.Dir(expectedAbs), 0o755); err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if file.Executable {
		mode = 0o755
	}
	if err := os.WriteFile(expectedAbs, content, mode); err != nil {
		return err
	}
	if file.Executable {
		_ = os.Chmod(expectedAbs, 0o755)
	}
	state.Files[entry.Identity] = StateFile{Path: expectedRel, Src: file.Src, SHA256: newHash}
	fmt.Fprintf(out, "wrote-file: %s\n", filePath)
	return nil
}

func ensureLinkFile(out io.Writer, root string, model *Model, state *State, filePath string, allowMove bool) error {
	entry := model.Files[filePath]
	file := entry.File
	targetEntry, ok := model.Files[file.Link]
	if !ok {
		return fmt.Errorf("link target is not defined: %s", file.Link)
	}
	if !pathExists(filepath.Join(root, targetEntry.Path)) {
		return fmt.Errorf("link target is missing: %s", targetEntry.Path)
	}

	stateFile, hasState := state.Files[entry.Identity]
	expectedRel := entry.Path
	expectedAbs := filepath.Join(root, expectedRel)
	expectedTarget, err := relativeSymlinkTarget(expectedRel, targetEntry.Path)
	if err != nil {
		return err
	}

	if hasState && stateFile.Path != expectedRel {
		oldAbs := filepath.Join(root, stateFile.Path)
		if pathExists(oldAbs) {
			if !allowMove {
				return fmt.Errorf("already written at %s; run jig sync to move it", stateFile.Path)
			}
			if pathExists(expectedAbs) {
				return fmt.Errorf("target path already exists: %s", expectedRel)
			}
			if err := os.MkdirAll(filepath.Dir(expectedAbs), 0o755); err != nil {
				return err
			}
			if err := os.Rename(oldAbs, expectedAbs); err != nil {
				return err
			}
			pruneEmptyParents(root, filepath.Dir(stateFile.Path))
			fmt.Fprintf(out, "moved-file: %s: %s -> %s\n", filePath, stateFile.Path, expectedRel)
			hasState = true
		} else {
			delete(state.Files, entry.Identity)
			hasState = false
		}
	}

	if pathExists(expectedAbs) {
		info, err := os.Lstat(expectedAbs)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink == 0 {
			return fmt.Errorf("expected symlink path exists and is not a symlink: %s", expectedRel)
		}
		currentTarget, err := os.Readlink(expectedAbs)
		if err != nil {
			return err
		}
		if currentTarget == expectedTarget {
			state.Files[entry.Identity] = StateFile{Path: expectedRel, Link: file.Link}
			fmt.Fprintf(out, "present-file: %s\n", filePath)
			return nil
		}
		if !hasState || stateFile.Link == "" {
			return fmt.Errorf("existing symlink has different target")
		}
		if err := os.Remove(expectedAbs); err != nil {
			return err
		}
	}

	if err := os.MkdirAll(filepath.Dir(expectedAbs), 0o755); err != nil {
		return err
	}
	if err := os.Symlink(expectedTarget, expectedAbs); err != nil {
		return err
	}
	state.Files[entry.Identity] = StateFile{Path: expectedRel, Link: file.Link}
	fmt.Fprintf(out, "linked-file: %s\n", filePath)
	return nil
}

func relativeSymlinkTarget(linkPath string, targetPath string) (string, error) {
	fromDir := filepath.Dir(linkPath)
	if fromDir == "." {
		fromDir = ""
	}
	rel, err := filepath.Rel(fromDir, targetPath)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

func pruneEmptyParents(root string, relDir string) {
	if relDir == "." || relDir == "" {
		return
	}
	for {
		if relDir == "." || relDir == "" {
			return
		}
		abs := filepath.Join(root, relDir)
		if filepath.Clean(abs) == filepath.Clean(root) {
			return
		}
		if err := os.Remove(abs); err != nil {
			return
		}
		relDir = filepath.Dir(relDir)
	}
}

func fetchFileContent(src string) ([]byte, error) {
	parsed, err := parseFileSrc(src)
	if err != nil {
		return nil, err
	}
	return fetchGitFile(parsed.GitURL, "", parsed.Path)
}

func installedDefinedRepos(root string, model *Model, state *State) []string {
	identityToPath := repoIdentityToPath(model)
	resultSet := map[string]bool{}
	for identity, stateRepo := range state.Repos {
		repoPath, ok := identityToPath[identity]
		if !ok {
			continue
		}
		if isGitRepo(filepath.Join(root, stateRepo.Path)) {
			resultSet[repoPath] = true
		}
	}
	for _, repoPath := range sortedRepoPaths(model) {
		if isGitRepo(filepath.Join(root, repoPath)) {
			resultSet[repoPath] = true
		}
	}
	return sortedKeys(resultSet)
}

func installedPath(root string, model *Model, state *State, repoPath string) (string, bool) {
	entry := model.Repos[repoPath]
	if stateRepo, ok := state.Repos[entry.Identity]; ok {
		abs := filepath.Join(root, stateRepo.Path)
		if isGitRepo(abs) {
			return abs, true
		}
	}
	expected := filepath.Join(root, entry.Path)
	if isGitRepo(expected) {
		return expected, true
	}
	return "", false
}

func installedRepoIdentitySet(root string, model *Model, state *State) map[string]bool {
	installed := map[string]bool{}
	identityToPath := repoIdentityToPath(model)
	for identity, stateRepo := range state.Repos {
		if _, ok := identityToPath[identity]; !ok {
			continue
		}
		if isGitRepo(filepath.Join(root, stateRepo.Path)) {
			installed[identity] = true
		}
	}
	for _, repoPath := range sortedRepoPaths(model) {
		entry := model.Repos[repoPath]
		if isGitRepo(filepath.Join(root, entry.Path)) {
			installed[entry.Identity] = true
		}
	}
	return installed
}

func reportStale(out io.Writer, root string, model *Model, state *State) {
	repoIdentityToPath := repoIdentityToPath(model)
	fileIdentityToPath := fileIdentityToPath(model)
	var stale []string
	for identity, stateRepo := range state.Repos {
		if _, ok := repoIdentityToPath[identity]; !ok && isGitRepo(filepath.Join(root, stateRepo.Path)) {
			stale = append(stale, fmt.Sprintf("%s at %s is no longer defined", identity, stateRepo.Path))
		}
	}
	for identity, stateFile := range state.Files {
		if _, ok := fileIdentityToPath[identity]; !ok && pathExists(filepath.Join(root, stateFile.Path)) {
			stale = append(stale, fmt.Sprintf("%s at %s is no longer defined", identity, stateFile.Path))
		}
	}
	printGroup(out, "stale", stale)
}

func printDefinitionChanges(out io.Writer, oldModel *Model, newModel *Model) {
	printEntryChanges(out, "repo", repoIdentityToPath(oldModel), repoIdentityToPath(newModel), repoChanged(oldModel, newModel))
	printEntryChanges(out, "file", fileIdentityToPath(oldModel), fileIdentityToPath(newModel), fileChanged(oldModel, newModel))
}

func printEntryChanges(out io.Writer, label string, oldByID map[string]string, newByID map[string]string, changedByID map[string]bool) {
	var added []string
	var removed []string
	var moved []string
	var changed []string
	for identity, newPath := range newByID {
		oldPath, ok := oldByID[identity]
		if !ok {
			added = append(added, newPath)
			continue
		}
		if oldPath != newPath {
			moved = append(moved, fmt.Sprintf("%s: %s -> %s", identity, oldPath, newPath))
		}
		if changedByID[identity] {
			changed = append(changed, newPath)
		}
	}
	for identity, oldPath := range oldByID {
		if _, ok := newByID[identity]; !ok {
			removed = append(removed, oldPath)
		}
	}
	printGroup(out, label+"-added", added)
	printGroup(out, label+"-removed", removed)
	printGroup(out, label+"-moved", moved)
	printGroup(out, label+"-changed", changed)
}

func repoChanged(oldModel *Model, newModel *Model) map[string]bool {
	result := map[string]bool{}
	oldByID := repoIdentityToPath(oldModel)
	newByID := repoIdentityToPath(newModel)
	for identity, newPath := range newByID {
		oldPath, ok := oldByID[identity]
		if !ok {
			continue
		}
		oldEntry := oldModel.Repos[oldPath]
		newEntry := newModel.Repos[newPath]
		oldRepo := oldEntry.Repo
		newRepo := newEntry.Repo
		if oldRepo.Git != newRepo.Git || oldRepo.Web != newRepo.Web || oldRepo.Description != newRepo.Description || !reflect.DeepEqual(oldRepo.DependsOn, newRepo.DependsOn) || !reflect.DeepEqual(oldEntry.Conditions, newEntry.Conditions) {
			result[identity] = true
		}
	}
	return result
}

func fileChanged(oldModel *Model, newModel *Model) map[string]bool {
	result := map[string]bool{}
	oldByID := fileIdentityToPath(oldModel)
	newByID := fileIdentityToPath(newModel)
	for identity, newPath := range newByID {
		oldPath, ok := oldByID[identity]
		if !ok {
			continue
		}
		oldEntry := oldModel.Files[oldPath]
		newEntry := newModel.Files[newPath]
		oldFile := oldEntry.File
		newFile := newEntry.File
		if oldFile.Src != newFile.Src || oldFile.Link != newFile.Link || oldFile.Description != newFile.Description || oldFile.Executable != newFile.Executable || !reflect.DeepEqual(oldEntry.Conditions, newEntry.Conditions) {
			result[identity] = true
		}
	}
	return result
}

func isGitRepo(path string) bool {
	if !pathExists(path) {
		return false
	}
	_, err := git(path, "rev-parse", "--is-inside-work-tree")
	return err == nil
}

func gitOrigin(path string) (string, error) {
	out, err := git(path, "remote", "get-url", "origin")
	return strings.TrimSpace(out), err
}

func isDirty(path string) bool {
	out, err := git(path, "status", "--porcelain")
	return err == nil && strings.TrimSpace(out) != ""
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return stdout.String(), errors.New(msg)
	}
	return stdout.String(), nil
}

func shortError(err error) string {
	msg := strings.TrimSpace(err.Error())
	if idx := strings.IndexByte(msg, '\n'); idx >= 0 {
		return msg[:idx]
	}
	return msg
}

func printGroup(out io.Writer, label string, items []string) {
	if len(items) == 0 {
		return
	}
	sort.Strings(items)
	fmt.Fprintf(out, "%s:\n", label)
	for _, item := range items {
		fmt.Fprintf(out, "  %s\n", item)
	}
}

func fileSHA256(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return sha256Hex(data), nil
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

type parsedArgs struct {
	Positionals []string
	Values      map[string]string
	Flags       map[string]bool
}

func parseArgs(args []string, valueFlags map[string]bool, boolFlags map[string]bool) (parsedArgs, error) {
	parsed := parsedArgs{Values: map[string]string{}, Flags: map[string]bool{}}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if valueFlags != nil && valueFlags[arg] {
			if i+1 >= len(args) {
				return parsed, fmt.Errorf("%s requires a value", arg)
			}
			parsed.Values[arg] = args[i+1]
			i++
			continue
		}
		if boolFlags != nil && boolFlags[arg] {
			parsed.Flags[arg] = true
			continue
		}
		if strings.HasPrefix(arg, "-") {
			return parsed, fmt.Errorf("unknown flag %s", arg)
		}
		parsed.Positionals = append(parsed.Positionals, arg)
	}
	return parsed, nil
}

func parseInitArgs(args []string) (parsedArgs, error) {
	parsed := parsedArgs{Values: map[string]string{}, Flags: map[string]bool{}}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--path":
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
				return parsed, errors.New("--path requires a value")
			}
			parsed.Values[arg] = args[i+1]
			i++
		case "--clone":
			parsed.Flags[arg] = true
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				parsed.Values[arg] = args[i+1]
				i++
			}
		case "--with-optional-deps":
			parsed.Flags[arg] = true
		default:
			if strings.HasPrefix(arg, "-") {
				return parsed, fmt.Errorf("unknown flag %s", arg)
			}
			parsed.Positionals = append(parsed.Positionals, arg)
		}
	}
	return parsed, nil
}
