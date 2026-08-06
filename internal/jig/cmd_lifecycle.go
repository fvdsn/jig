package jig

import (
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
)

// The lifecycle verbs — setup, fmt, lint, test — are a fixed vocabulary of
// short-lived, per-repo commands that terminate with a verdict. The schema
// declares each repository's own implementation so a mixed-technology fleet
// standardizes on the verb, not the tooling. Jig only ever runs them when
// the user invokes the matching command, never as a side effect of clone,
// sync, or update.

type LifecycleOptions struct {
	Path            string
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
	selection, err := ws.Select(NodeQuery{Path: options.Path, IncludeArchived: options.IncludeArchived, Tags: options.Tags})
	if err != nil {
		return err
	}

	type candidate struct {
		repoPath string
		local    string
		command  string
	}
	var candidates []candidate
	withoutCommand := 0
	for _, entry := range selection.ofKind(EntryRepo) {
		local, ok := installedPath(ws.Root, &ws.Model, &ws.State, entry.Path)
		if !ok {
			continue
		}
		command := lifecycleCommand(entry.Repo, verb)
		if command == "" {
			withoutCommand++
			continue
		}
		candidates = append(candidates, candidate{entry.Path, local, command})
	}

	var mu sync.Mutex
	var skipped []string
	run := func(i int) {
		output, err := runRepoCommand(candidates[i].local, candidates[i].command)
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			msg := err.Error()
			if output != "" {
				msg += "\n" + output
			}
			msg = strings.ReplaceAll(strings.TrimSpace(msg), "\n", "\n  ")
			skipped = append(skipped, fmt.Sprintf("%s: %s", candidates[i].repoPath, msg))
			return
		}
		fmt.Fprintf(out, "%s: %s\n", verb, candidates[i].repoPath)
	}
	if verb == "setup" {
		// A repository's setup may rely on its dependencies being set up
		// (a shared package built, a database created), so setup runs
		// sequentially in dependency order. The checking verbs are
		// independent and run in parallel.
		paths := make([]string, len(candidates))
		index := map[string]int{}
		for i, c := range candidates {
			paths[i] = c.repoPath
			index[c.repoPath] = i
		}
		for _, repoPath := range dependencyOrder(&ws.Model, paths) {
			run(index[repoPath])
		}
	} else {
		forEachParallel(len(candidates), run)
	}

	if withoutCommand > 0 {
		fmt.Fprintf(out, "%d repositories define no %s command\n", withoutCommand, verb)
	}
	printGroup(out, "skipped", skipped)
	if len(skipped) > 0 {
		return fmt.Errorf("%s failed in %d repositories", verb, len(skipped))
	}
	return nil
}

// runRepoCommand runs a schema-declared lifecycle command in the checkout
// through the shell, capturing combined output for failure reports.
func runRepoCommand(dir string, command string) (string, error) {
	cmd := exec.Command("sh", "-c", command)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(output)), err
}

// dependencyOrder orders the given repositories so dependencies come before
// their dependents, restricted to the given set. A depth-first postorder
// walk visits dependencies first and breaks cycles by skipping repositories
// already visited.
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
			selection, err := model.Select(NodeQuery{Path: dep.Path, IncludeArchived: true})
			if err != nil {
				continue
			}
			for _, match := range selection.ofKind(EntryRepo) {
				if inSet[match.Path] {
					visit(match.Path)
				}
			}
		}
		order = append(order, repoPath)
	}
	for _, path := range repoPaths {
		visit(path)
	}
	return order
}
