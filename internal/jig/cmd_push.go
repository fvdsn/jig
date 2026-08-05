package jig

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
)

type PushOptions struct {
	Path            string
	IncludeArchived bool
	Tags            []string
	SetUpstream     bool // create the upstream (git push -u) when the branch has none
}

// Push publishes the current branch of installed repositories matching the
// query, mirroring git push across the workspace. Pushes are never forced;
// a repository where the remote rejects the push is reported as skipped.
// Repositories with nothing to push report "up to date" without touching
// the network.
func Push(options PushOptions, out io.Writer) error {
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
		verb, err := pushRepo(candidates[i].local, options.SetUpstream)
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			msg := strings.ReplaceAll(strings.TrimSpace(err.Error()), "\n", "\n  ")
			skipped = append(skipped, fmt.Sprintf("%s: %s", candidates[i].repoPath, msg))
			return
		}
		fmt.Fprintf(out, "%s: %s\n", verb, candidates[i].repoPath)
	})
	printGroup(out, "skipped", skipped)
	if len(skipped) > 0 {
		return fmt.Errorf("%d repositories were skipped", len(skipped))
	}
	return nil
}

// pushRepo pushes one repository's current branch and returns the report
// verb. A branch without an upstream is pushed with -u when setUpstream is
// set, so re-running is idempotent: the first push creates the upstream and
// later runs report up to date.
func pushRepo(local string, setUpstream bool) (string, error) {
	branch := gitBranch(local)
	if branch == "" || strings.HasPrefix(branch, "@") {
		return "", errors.New("detached HEAD, nothing to push")
	}
	ahead, _, hasUpstream := aheadBehind(local)
	if hasUpstream {
		if ahead == 0 {
			return "up to date", nil
		}
		if _, err := git(local, "push", "--quiet"); err != nil {
			return "", err
		}
		return "pushed", nil
	}
	if !setUpstream {
		return "", errors.New("no upstream branch (use -u to set one)")
	}
	if _, err := git(local, "push", "--quiet", "-u", "origin", branch); err != nil {
		return "", err
	}
	return "pushed", nil
}
