package main

import (
	"bytes"
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
	"unicode"
)

const (
	definitionFile = ".jig.json"
	stateFile      = ".jig/state.json"
)

type Definition struct {
	Version int             `json:"version"`
	Source  *Source         `json:"source,omitempty"`
	Repos   map[string]Repo `json:"repos"`
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
}

type Dependency struct {
	Path     string `json:"path"`
	Optional bool   `json:"optional,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

type State struct {
	Version int                  `json:"version"`
	Repos   map[string]StateRepo `json:"repos"`
}

type StateRepo struct {
	Path string `json:"path"`
	Git  string `json:"git,omitempty"`
}

type Workspace struct {
	Root  string
	Def   Definition
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
	case "update":
		return cmdUpdate(out)
	case "help", "--help", "-h":
		printUsage(out)
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func printUsage(out io.Writer) {
	fmt.Fprintln(out, "Jig is a workspace CLI for managing many related Git repositories from a shared .jig.json definition.")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "It can clone repositories with their dependencies, keep local checkouts aligned with the definition, pull installed repositories, and report workspace status.")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Usage:")
	fmt.Fprintln(out, "  jig <command> [args]")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Commands:")
	fmt.Fprintln(out, "  init <git-url> [workspace-dir] [--path <path>] [--clone <path>] [--with-optional-deps]")
	fmt.Fprintln(out, "      Initialize a workspace from a Git-hosted Jig definition, optionally cloning a path.")
	fmt.Fprintln(out, "  validate")
	fmt.Fprintln(out, "      Validate the current workspace .jig.json file.")
	fmt.Fprintln(out, "  list")
	fmt.Fprintln(out, "      List repositories defined in .jig.json.")
	fmt.Fprintln(out, "  info <path>")
	fmt.Fprintln(out, "      Show repository metadata or repositories under a group path.")
	fmt.Fprintln(out, "  deps <path>")
	fmt.Fprintln(out, "      Show expanded recursive dependencies for repositories matching a path.")
	fmt.Fprintln(out, "  clone <path> [--with-optional-deps]")
	fmt.Fprintln(out, "      Clone repositories matching a path and their non-optional dependencies.")
	fmt.Fprintln(out, "  sync [path] [--with-optional-deps]")
	fmt.Fprintln(out, "      Clone missing repos, move renamed repos, update origins, and refresh local state.")
	fmt.Fprintln(out, "  pull [path]")
	fmt.Fprintln(out, "      Run git pull in installed repositories matching a path or group.")
	fmt.Fprintln(out, "  status [path]")
	fmt.Fprintln(out, "      Show installed, missing, moved, dirty, stale, and remote-changed repositories.")
	fmt.Fprintln(out, "  update")
	fmt.Fprintln(out, "      Update .jig.json from its configured source without changing local checkouts.")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Paths identify repositories or groups. Current specs use slash paths such as services/checkout or platform.")
}

func cmdInit(args []string, out io.Writer) error {
	parsed, err := parseArgs(args, map[string]bool{"--path": true, "--clone": true}, map[string]bool{"--with-optional-deps": true})
	if err != nil {
		return err
	}
	if len(parsed.Positionals) == 0 || len(parsed.Positionals) > 2 {
		return errors.New("usage: jig init <git-url> [workspace-dir] [--path <path>] [--clone <path>] [--with-optional-deps]")
	}

	gitURL := parsed.Positionals[0]
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

	if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
		return err
	}
	localDefinitionPath := filepath.Join(workspaceDir, definitionFile)
	if _, err := os.Stat(localDefinitionPath); err == nil {
		return errors.New("workspace already initialized: .jig.json exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	ref, err := discoverDefaultBranch(gitURL)
	if err != nil {
		return fmt.Errorf("could not determine default branch for %s", gitURL)
	}

	def, err := fetchDefinition(gitURL, ref, definitionPath)
	if err != nil {
		return err
	}
	def.Source = &Source{Type: "git", URL: gitURL, Ref: ref, Path: definitionPath}

	validation := validateDefinition(def)
	if len(validation.Errors) > 0 {
		return validation.asError("invalid fetched definition")
	}

	if err := writeJSON(localDefinitionPath, def); err != nil {
		return err
	}
	if err := saveState(workspaceDir, emptyState()); err != nil {
		return err
	}

	fmt.Fprintf(out, "initialized workspace at %s\n", workspaceDir)
	if clonePath := parsed.Values["--clone"]; clonePath != "" {
		state, err := loadState(workspaceDir)
		if err != nil {
			return err
		}
		ws := Workspace{Root: workspaceDir, Def: *def, State: state}
		if err := clonePathIntoWorkspace(out, &ws, clonePath, parsed.Flags["--with-optional-deps"]); err != nil {
			return err
		}
		if err := saveState(workspaceDir, ws.State); err != nil {
			return err
		}
	}
	return nil
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
	for _, repoPath := range sortedRepoPaths(&ws.Def) {
		repo := ws.Def.Repos[repoPath]
		if repo.Description == "" {
			fmt.Fprintln(out, repoPath)
		} else {
			fmt.Fprintf(out, "%s\t%s\n", repoPath, repo.Description)
		}
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
	if repo, ok := ws.Def.Repos[path]; ok {
		fmt.Fprintf(out, "path: %s\n", path)
		fmt.Fprintf(out, "identity: %s\n", repoIdentity(path, repo))
		fmt.Fprintf(out, "git: %s\n", repo.Git)
		if repo.Web != "" {
			fmt.Fprintf(out, "web: %s\n", repo.Web)
		}
		if repo.Description != "" {
			fmt.Fprintf(out, "description: %s\n", repo.Description)
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

	matches := matchingRepos(&ws.Def, path)
	if len(matches) == 0 {
		return fmt.Errorf("no repository or group matches %q", path)
	}
	fmt.Fprintf(out, "group: %s\n", path)
	fmt.Fprintln(out, "repos:")
	for _, repoPath := range matches {
		fmt.Fprintf(out, "  %s\n", repoPath)
	}
	return nil
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
	roots := matchingRepos(&ws.Def, path)
	if len(roots) == 0 {
		return fmt.Errorf("no repositories match %q", path)
	}
	deps, err := resolveRepoSet(&ws.Def, roots, parsed.Flags["--with-optional-deps"], false)
	if err != nil {
		return err
	}
	for _, dep := range deps {
		fmt.Fprintln(out, dep)
	}
	return nil
}

func cmdClone(args []string, out io.Writer) error {
	parsed, err := parseArgs(args, nil, map[string]bool{"--with-optional-deps": true})
	if err != nil {
		return err
	}
	if len(parsed.Positionals) != 1 {
		return errors.New("usage: jig clone <path> [--with-optional-deps]")
	}
	ws, err := loadWorkspace(true)
	if err != nil {
		return err
	}
	if err := clonePathIntoWorkspace(out, ws, parsed.Positionals[0], parsed.Flags["--with-optional-deps"]); err != nil {
		return err
	}
	return saveState(ws.Root, ws.State)
}

func clonePathIntoWorkspace(out io.Writer, ws *Workspace, path string, includeOptional bool) error {
	roots := matchingRepos(&ws.Def, path)
	if len(roots) == 0 {
		return fmt.Errorf("no repositories match %q", path)
	}
	set, err := resolveRepoSet(&ws.Def, roots, includeOptional, true)
	if err != nil {
		return err
	}
	for _, repoPath := range set {
		if err := ensureRepo(out, ws.Root, &ws.Def, &ws.State, repoPath, false); err != nil {
			fmt.Fprintf(out, "skipped:\n  %s: %s\n", repoPath, err)
		}
	}
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

	var roots []string
	if len(parsed.Positionals) == 1 {
		roots = matchingRepos(&ws.Def, parsed.Positionals[0])
		if len(roots) == 0 {
			return fmt.Errorf("no repositories match %q", parsed.Positionals[0])
		}
	} else {
		roots = installedDefinedRepos(ws.Root, &ws.Def, &ws.State)
	}

	set, err := resolveRepoSetForSync(ws.Root, &ws.Def, &ws.State, roots, parsed.Flags["--with-optional-deps"])
	if err != nil {
		return err
	}
	for _, path := range set {
		if err := ensureRepo(out, ws.Root, &ws.Def, &ws.State, path, true); err != nil {
			fmt.Fprintf(out, "skipped:\n  %s: %s\n", path, err)
		}
	}
	reportStale(out, ws.Root, &ws.Def, &ws.State)
	return saveState(ws.Root, ws.State)
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
	}

	var pulled []string
	var skipped []string
	for _, repoPath := range sortedRepoPaths(&ws.Def) {
		if filter != "" && !pathMatches(filter, repoPath) {
			continue
		}
		local, ok := installedPath(ws.Root, &ws.Def, &ws.State, repoPath)
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
	}

	var installed []string
	var missing []string
	var moved []string
	var remoteChanged []string
	var dirty []string
	var conflicts []string
	identityToPath := repoIdentityToPath(&ws.Def)

	for _, repoPath := range sortedRepoPaths(&ws.Def) {
		if filter != "" && !pathMatches(filter, repoPath) {
			continue
		}
		repo := ws.Def.Repos[repoPath]
		identity := repoIdentity(repoPath, repo)
		expectedRel := repoLocalPath(repoPath)
		expectedAbs := filepath.Join(ws.Root, expectedRel)
		stateRepo, hasState := ws.State.Repos[identity]

		if hasState && stateRepo.Path != expectedRel && isGitRepo(filepath.Join(ws.Root, stateRepo.Path)) {
			moved = append(moved, fmt.Sprintf("%s: %s -> %s", repoPath, stateRepo.Path, expectedRel))
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
		if err != nil || origin != repo.Git {
			remoteChanged = append(remoteChanged, fmt.Sprintf("%s: %s -> %s", repoPath, origin, repo.Git))
		}
		if isDirty(expectedAbs) {
			dirty = append(dirty, repoPath)
		}
		installed = append(installed, repoPath)
	}

	var stale []string
	if filter == "" {
		for identity, stateRepo := range ws.State.Repos {
			if _, ok := identityToPath[identity]; !ok {
				stale = append(stale, fmt.Sprintf("%s at %s is no longer defined", identity, stateRepo.Path))
			}
		}
		sort.Strings(stale)
	}

	printGroup(out, "installed", installed)
	printGroup(out, "moved", moved)
	printGroup(out, "missing", missing)
	printGroup(out, "remote-changed", remoteChanged)
	printGroup(out, "dirty", dirty)
	printGroup(out, "conflicts", conflicts)
	printGroup(out, "stale", stale)
	return nil
}

func cmdUpdate(out io.Writer) error {
	ws, err := loadWorkspace(false)
	if err != nil {
		return err
	}
	if ws.Def.Source == nil {
		return errors.New(".jig.json has no source")
	}
	source := *ws.Def.Source
	if source.Type != "git" {
		return fmt.Errorf("unsupported source type %q", source.Type)
	}
	if source.Path == "" {
		source.Path = definitionFile
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
	printDefinitionChanges(out, &ws.Def, incoming)
	return writeJSON(filepath.Join(ws.Root, definitionFile), incoming)
}

func validateDefinition(def *Definition) validationResult {
	var result validationResult
	if def.Version != 1 {
		result.Errors = append(result.Errors, "unsupported or missing version")
	}
	if def.Repos == nil {
		result.Errors = append(result.Errors, "missing repos")
	}
	if def.Source != nil {
		if def.Source.Type != "git" {
			result.Errors = append(result.Errors, "source.type must be git")
		}
		if def.Source.URL == "" {
			result.Errors = append(result.Errors, "source.url is required")
		}
	}

	identityToPath := map[string]string{}
	for repoPath, repo := range def.Repos {
		if err := validateRepoPath(repoPath); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("invalid repository path %q: %s", repoPath, err))
		}
		if repo.Git == "" {
			result.Errors = append(result.Errors, fmt.Sprintf("repository %s missing git", repoPath))
		}
		identity := repoIdentity(repoPath, repo)
		if prev, ok := identityToPath[identity]; ok {
			result.Errors = append(result.Errors, fmt.Sprintf("duplicate repository identity %s: %s and %s", identity, prev, repoPath))
		} else {
			identityToPath[identity] = repoPath
		}
		for _, dep := range repo.DependsOn {
			if dep.Path == "" {
				result.Errors = append(result.Errors, fmt.Sprintf("repository %s has dependency with empty path", repoPath))
				continue
			}
			if len(matchingRepos(def, dep.Path)) == 0 {
				result.Errors = append(result.Errors, fmt.Sprintf("repository %s dependency %s does not resolve to any repository", repoPath, dep.Path))
			}
		}
	}

	for _, cycle := range detectCycles(def) {
		result.Warnings = append(result.Warnings, "dependency cycle detected: "+strings.Join(cycle, " -> "))
	}
	return result
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

func validateRepoPath(path string) error {
	if path == "" {
		return errors.New("empty path")
	}
	if strings.Contains(path, "/") || strings.Contains(path, string(os.PathSeparator)) {
		return errors.New("must not contain slashes")
	}
	if strings.HasPrefix(path, ".") || strings.HasSuffix(path, ".") {
		return errors.New("must not start or end with a dot")
	}
	for _, segment := range strings.Split(path, ".") {
		if segment == "" {
			return errors.New("must not contain empty segments")
		}
		for _, r := range segment {
			if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' {
				continue
			}
			return fmt.Errorf("invalid character %q", r)
		}
	}
	return nil
}

func detectCycles(def *Definition) [][]string {
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
		for _, dep := range def.Repos[repoPath].DependsOn {
			for _, match := range matchingRepos(def, dep.Path) {
				visit(match)
			}
		}
		stack = stack[:len(stack)-1]
		visited[repoPath] = 2
	}

	for _, repoPath := range sortedRepoPaths(def) {
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

func resolveDependencies(def *Definition, repoPath string, includeOptional bool) ([]string, error) {
	set, err := resolveRepoSet(def, []string{repoPath}, includeOptional, false)
	if err != nil {
		return nil, err
	}
	return set, nil
}

func resolveRepoSet(def *Definition, roots []string, includeOptional bool, includeRoots bool) ([]string, error) {
	visited := map[string]bool{}
	resultSet := map[string]bool{}
	excludedIdentities := map[string]bool{}
	if !includeRoots {
		for _, root := range roots {
			if repo, ok := def.Repos[root]; ok {
				excludedIdentities[repoIdentity(root, repo)] = true
			}
		}
	}

	var visit func(string) error
	visit = func(repoPath string) error {
		repo, ok := def.Repos[repoPath]
		if !ok {
			return fmt.Errorf("unknown repository %q", repoPath)
		}
		identity := repoIdentity(repoPath, repo)
		if visited[identity] {
			return nil
		}
		visited[identity] = true
		for _, dep := range repo.DependsOn {
			if dep.Optional && !includeOptional {
				continue
			}
			matches := matchingRepos(def, dep.Path)
			if len(matches) == 0 {
				return fmt.Errorf("dependency %s for %s does not resolve to any repository", dep.Path, repoPath)
			}
			for _, match := range matches {
				matchIdentity := repoIdentity(match, def.Repos[match])
				if !excludedIdentities[matchIdentity] {
					resultSet[match] = true
				}
				if err := visit(match); err != nil {
					return err
				}
			}
		}
		return nil
	}

	for _, root := range roots {
		if includeRoots {
			resultSet[root] = true
		}
		if err := visit(root); err != nil {
			return nil, err
		}
	}

	var result []string
	for repoPath := range resultSet {
		result = append(result, repoPath)
	}
	sort.Strings(result)
	return result, nil
}

func resolveRepoSetForSync(root string, def *Definition, state *State, roots []string, includeOptional bool) ([]string, error) {
	installed := installedRepoIdentitySet(root, def, state)
	visited := map[string]bool{}
	resultSet := map[string]bool{}

	var visit func(string) error
	visit = func(repoPath string) error {
		repo, ok := def.Repos[repoPath]
		if !ok {
			return fmt.Errorf("unknown repository %q", repoPath)
		}
		identity := repoIdentity(repoPath, repo)
		if visited[identity] {
			return nil
		}
		visited[identity] = true
		for _, dep := range repo.DependsOn {
			matches := matchingRepos(def, dep.Path)
			if len(matches) == 0 {
				return fmt.Errorf("dependency %s for %s does not resolve to any repository", dep.Path, repoPath)
			}
			for _, match := range matches {
				matchIdentity := repoIdentity(match, def.Repos[match])
				if dep.Optional && !includeOptional && !installed[matchIdentity] {
					continue
				}
				resultSet[match] = true
				if err := visit(match); err != nil {
					return err
				}
			}
		}
		return nil
	}

	for _, rootRepo := range roots {
		resultSet[rootRepo] = true
		if err := visit(rootRepo); err != nil {
			return nil, err
		}
	}

	var result []string
	for repoPath := range resultSet {
		result = append(result, repoPath)
	}
	sort.Strings(result)
	return result, nil
}

func installedRepoIdentitySet(root string, def *Definition, state *State) map[string]bool {
	installed := map[string]bool{}
	identityToPath := repoIdentityToPath(def)
	for identity, stateRepo := range state.Repos {
		if _, ok := identityToPath[identity]; !ok {
			continue
		}
		if isGitRepo(filepath.Join(root, stateRepo.Path)) {
			installed[identity] = true
		}
	}
	for _, repoPath := range sortedRepoPaths(def) {
		repo := def.Repos[repoPath]
		if isGitRepo(filepath.Join(root, repoLocalPath(repoPath))) {
			installed[repoIdentity(repoPath, repo)] = true
		}
	}
	return installed
}

func matchingRepos(def *Definition, path string) []string {
	var matches []string
	for _, repoPath := range sortedRepoPaths(def) {
		if pathMatches(path, repoPath) {
			matches = append(matches, repoPath)
		}
	}
	return matches
}

func pathMatches(path string, repoPath string) bool {
	return repoPath == path || strings.HasPrefix(repoPath, path+".")
}

func sortedRepoPaths(def *Definition) []string {
	paths := make([]string, 0, len(def.Repos))
	for path := range def.Repos {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func repoIdentity(path string, repo Repo) string {
	if repo.ID != "" {
		return repo.ID
	}
	return path
}

func repoIdentityToPath(def *Definition) map[string]string {
	result := map[string]string{}
	for path, repo := range def.Repos {
		result[repoIdentity(path, repo)] = path
	}
	return result
}

func repoLocalPath(repoPath string) string {
	return filepath.Join(strings.Split(repoPath, ".")...)
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
	state := emptyState()
	if withState {
		state, err = loadState(root)
		if err != nil {
			return nil, err
		}
	} else {
		state, _ = loadState(root)
	}
	return &Workspace{Root: root, Def: *def, State: state}, nil
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
	return State{Version: 1, Repos: map[string]StateRepo{}}
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
	return state, nil
}

func saveState(root string, state State) error {
	if state.Version == 0 {
		state.Version = 1
	}
	if state.Repos == nil {
		state.Repos = map[string]StateRepo{}
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
	if definitionPath == "" {
		definitionPath = definitionFile
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
	return loadDefinition(filepath.Join(repoDir, definitionPath))
}

func ensureRepo(out io.Writer, root string, def *Definition, state *State, repoPath string, allowMove bool) error {
	repo := def.Repos[repoPath]
	identity := repoIdentity(repoPath, repo)
	expectedRel := repoLocalPath(repoPath)
	expectedAbs := filepath.Join(root, expectedRel)
	stateRepo, hasState := state.Repos[identity]

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
			fmt.Fprintf(out, "moved: %s: %s -> %s\n", repoPath, stateRepo.Path, expectedRel)
			stateRepo.Path = expectedRel
			state.Repos[identity] = stateRepo
			hasState = true
		} else {
			delete(state.Repos, identity)
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
		state.Repos[identity] = StateRepo{Path: expectedRel, Git: repo.Git}
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
		if allowMove && hasState && state.Repos[identity].Path == expectedRel {
			if _, err := git(expectedAbs, "remote", "set-url", "origin", repo.Git); err != nil {
				return err
			}
			state.Repos[identity] = StateRepo{Path: expectedRel, Git: repo.Git}
			fmt.Fprintf(out, "updated-origin: %s\n", repoPath)
			return nil
		}
		return fmt.Errorf("existing Git repository has different origin at %s", expectedRel)
	}

	state.Repos[identity] = StateRepo{Path: expectedRel, Git: repo.Git}
	fmt.Fprintf(out, "present: %s\n", repoPath)
	return nil
}

func installedDefinedRepos(root string, def *Definition, state *State) []string {
	identityToPath := repoIdentityToPath(def)
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
	for _, repoPath := range sortedRepoPaths(def) {
		expected := filepath.Join(root, repoLocalPath(repoPath))
		if isGitRepo(expected) {
			resultSet[repoPath] = true
		}
	}
	var result []string
	for repoPath := range resultSet {
		result = append(result, repoPath)
	}
	sort.Strings(result)
	return result
}

func installedPath(root string, def *Definition, state *State, repoPath string) (string, bool) {
	repo := def.Repos[repoPath]
	identity := repoIdentity(repoPath, repo)
	if stateRepo, ok := state.Repos[identity]; ok {
		abs := filepath.Join(root, stateRepo.Path)
		if isGitRepo(abs) {
			return abs, true
		}
	}
	expected := filepath.Join(root, repoLocalPath(repoPath))
	if isGitRepo(expected) {
		return expected, true
	}
	return "", false
}

func reportStale(out io.Writer, root string, def *Definition, state *State) {
	identityToPath := repoIdentityToPath(def)
	var stale []string
	for identity, stateRepo := range state.Repos {
		if _, ok := identityToPath[identity]; !ok && isGitRepo(filepath.Join(root, stateRepo.Path)) {
			stale = append(stale, fmt.Sprintf("%s at %s is no longer defined", identity, stateRepo.Path))
		}
	}
	printGroup(out, "stale", stale)
}

func printDefinitionChanges(out io.Writer, oldDef *Definition, newDef *Definition) {
	oldByID := map[string]string{}
	newByID := map[string]string{}
	for path, repo := range oldDef.Repos {
		oldByID[repoIdentity(path, repo)] = path
	}
	for path, repo := range newDef.Repos {
		newByID[repoIdentity(path, repo)] = path
	}

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
		oldRepo := oldDef.Repos[oldPath]
		newRepo := newDef.Repos[newPath]
		if oldRepo.Git != newRepo.Git || oldRepo.Web != newRepo.Web || !reflect.DeepEqual(oldRepo.DependsOn, newRepo.DependsOn) || oldRepo.Description != newRepo.Description {
			changed = append(changed, newPath)
		}
	}
	for identity, oldPath := range oldByID {
		if _, ok := newByID[identity]; !ok {
			removed = append(removed, oldPath)
		}
	}

	printGroup(out, "added", added)
	printGroup(out, "removed", removed)
	printGroup(out, "moved", moved)
	printGroup(out, "changed", changed)
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
