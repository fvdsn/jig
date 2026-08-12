package jig

import (
	"fmt"
	"io"
	"strings"
	"sync"
)

type PullOptions struct {
	Path            string
	Id              string // selects one entry by identity instead of a path
	IncludeArchived bool
	Tags            []string
}

func Pull(options PullOptions, out io.Writer) error {
	return runGitInInstalled(out, NodeQuery{Path: options.Path, Id: options.Id, IncludeArchived: options.IncludeArchived, Tags: options.Tags}, "pulled", "pull", "--ff-only")
}

type FetchOptions struct {
	Path            string
	Id              string // selects one entry by identity instead of a path
	IncludeArchived bool
	Tags            []string
}

func Fetch(options FetchOptions, out io.Writer) error {
	return runGitInInstalled(out, NodeQuery{Path: options.Path, Id: options.Id, IncludeArchived: options.IncludeArchived, Tags: options.Tags}, "fetched", "fetch")
}

// runGitInInstalled runs a git command across the installed repositories
// matching the query, in parallel, printing one <verb>: line per success as
// it completes and a skipped group for failures.
func runGitInInstalled(out io.Writer, query NodeQuery, verb string, gitArgs ...string) error {
	ws, err := loadWorkspace(false)
	if err != nil {
		return err
	}
	selection, err := ws.Select(query)
	if err != nil {
		return err
	}

	type candidate struct {
		repoPath string
		local    string
	}
	var candidates []candidate
	for _, entry := range selection.ofKind(EntryRepo) {
		if local, ok := installedPath(ws.Root, &ws.Model, &ws.State, entry.Path); ok {
			candidates = append(candidates, candidate{entry.Path, local})
		}
	}

	var mu sync.Mutex
	var skipped []string
	forEachParallel(len(candidates), func(i int) {
		if _, err := git(candidates[i].local, gitArgs...); err != nil {
			msg := strings.ReplaceAll(strings.TrimSpace(err.Error()), "\n", "\n  ")
			mu.Lock()
			skipped = append(skipped, fmt.Sprintf("%s: %s", candidates[i].repoPath, msg))
			mu.Unlock()
			return
		}
		mu.Lock()
		fmt.Fprintf(out, "%s: %s\n", verb, candidates[i].repoPath)
		mu.Unlock()
	})
	printGroup(out, "skipped", skipped)
	if len(skipped) > 0 {
		return fmt.Errorf("%d repositories were skipped", len(skipped))
	}
	return nil
}
