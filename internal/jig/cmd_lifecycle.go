package jig

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// The lifecycle verbs — setup, fmt, lint, test — are a fixed vocabulary of
// short-lived, per-repo commands that terminate with a verdict. The schema
// declares each repository's own implementation so a mixed-technology fleet
// standardizes on the verb, not the tooling. Jig only ever runs them when
// the user invokes the matching command, never as a side effect of clone,
// sync, or update.

type LifecycleOptions struct {
	Path            string
	Id              string // selects one entry by identity instead of a path
	IncludeArchived bool
	Tags            []string
}

// Setup runs each repository's setup command in dependency order, so a
// fresh clone can be brought to a usable state in one command.
func Setup(options LifecycleOptions, out io.Writer) error {
	return runLifecycle("setup", options, out)
}

func Fmt(options LifecycleOptions, out io.Writer) error {
	return runLifecycle("fmt", options, out)
}

func Lint(options LifecycleOptions, out io.Writer) error {
	return runLifecycle("lint", options, out)
}

func Test(options LifecycleOptions, out io.Writer) error {
	return runLifecycle("test", options, out)
}

func lifecycleCommand(repo *Repo, verb string) string {
	switch verb {
	case "setup":
		return repo.Setup
	case "fmt":
		return repo.Fmt
	case "lint":
		return repo.Lint
	case "test":
		return repo.Test
	default:
		return ""
	}
}

func runLifecycle(verb string, options LifecycleOptions, out io.Writer) error {
	ws, err := loadWorkspace(false)
	if err != nil {
		return err
	}
	installed, err := selectInstalledRepos(ws, NodeQuery{Path: options.Path, Id: options.Id, IncludeArchived: options.IncludeArchived, Tags: options.Tags})
	if err != nil {
		return err
	}
	commands := map[string]string{}
	var repos []installedRepo
	withoutCommand := 0
	for _, repo := range installed {
		entry, _ := ws.Model.entry(repo.Path, EntryRepo)
		command := lifecycleCommand(entry.Repo, verb)
		if command == "" {
			withoutCommand++
			continue
		}
		commands[repo.Path] = command
		repos = append(repos, repo)
	}

	run := repoRun{Verb: verb, Label: "failed"}
	if verb == "setup" {
		// A repository's setup may rely on its dependencies being set up
		// (a shared package built, a database created), so setup runs
		// sequentially in dependency order. The checking verbs are
		// independent and run in parallel.
		paths := make([]string, len(repos))
		for i, repo := range repos {
			paths[i] = repo.Path
		}
		run.Order = dependencyOrder(&ws.Model, paths)
	}
	if withoutCommand > 0 {
		fmt.Fprintf(out, "%d repositories define no %s command\n", withoutCommand, verb)
	}
	return runInRepos(out, repos, run, func(repo installedRepo) (string, string, error) {
		output, err := runRepoCommand(repo.Local, commands[repo.Path])
		if err != nil && output != "" {
			// The failure group carries the command's output.
			err = fmt.Errorf("%s\n%s", err, output)
		}
		return verb, "", err
	})
}

// runRepoCommand runs a schema-declared lifecycle command in the checkout
// through the shell, capturing combined output for failure reports.
func runRepoCommand(dir string, command string) (string, error) {
	cmd := exec.Command(shellPath(), "-c", command)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(output)), err
}

// shellPath locates the sh that runs lifecycle commands. Schemas declare
// commands in sh syntax on every platform; Windows has no sh on PATH outside
// git bash, but Git for Windows ships one next to the git executable.
func shellPath() string {
	if runtime.GOOS != "windows" {
		return "sh"
	}
	if path, err := exec.LookPath("sh"); err == nil {
		return path
	}
	if gitPath, err := exec.LookPath("git"); err == nil {
		root := filepath.Dir(filepath.Dir(gitPath))
		for _, candidate := range []string{
			filepath.Join(root, "bin", "sh.exe"),
			filepath.Join(root, "usr", "bin", "sh.exe"),
		} {
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				return candidate
			}
		}
	}
	return "sh"
}

// dependencyOrder sorts repoPaths so that every repository comes after the
// repositories it depends on. The walk follows the model's full dependency
// graph, not only edges between the given repositories, so a dependency
// chain through a repository outside the set (one without a setup command,
// or not installed) still orders the two ends correctly. Only the given
// repositories appear in the result; cycles are broken by skipping
// repositories already visited.
func dependencyOrder(model *Model, repoPaths []string) []string {
	inSet := map[string]bool{}
	for _, path := range repoPaths {
		inSet[path] = true
	}
	visited := map[string]bool{}
	var order []string
	var visit func(string)
	visit = func(repoPath string) {
		if visited[repoPath] {
			return
		}
		visited[repoPath] = true
		entry, ok := model.entry(repoPath, EntryRepo)
		if !ok {
			return
		}
		for _, dep := range entry.Repo.DependsOn {
			for _, match := range model.resolveRepoRef(dep.Ref) {
				visit(match.Path)
			}
		}
		if inSet[repoPath] {
			order = append(order, repoPath)
		}
	}
	for _, path := range repoPaths {
		visit(path)
	}
	return order
}
