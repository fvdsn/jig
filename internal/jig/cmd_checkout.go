package jig

import (
	"fmt"
	"io"
	"strings"
	"sync"
)

type CheckoutOptions struct {
	Branch          string
	Path            string
	Create          bool // create the branch (git checkout -b) when it does not exist
	IncludeArchived bool
	Tags            []string
}

// Checkout switches installed repositories matching the query to a branch,
// mirroring git checkout across the workspace. Safety is git's own: a plain
// (non-forced) checkout never discards local changes, so repositories where
// the switch would lose work fail and are reported as skipped.
func Checkout(options CheckoutOptions, out io.Writer) error {
	// --branch applies the stricter branch-name rules (e.g. no leading
	// dash), which also keeps the name from being parsed as a git option.
	if _, err := git("", "check-ref-format", "--branch", options.Branch); err != nil {
		return fmt.Errorf("invalid branch name %q", options.Branch)
	}
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
		verb, err := checkoutRepo(candidates[i].local, options.Branch, options.Create)
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

// checkoutRepo switches one repository and returns the report verb. Creating
// is idempotent: when the branch already exists, -b degrades to a plain
// switch instead of failing.
func checkoutRepo(local string, branch string, create bool) (string, error) {
	if gitBranch(local) == branch {
		return "present", nil
	}
	if create && !localBranchExists(local, branch) {
		if _, err := git(local, "checkout", "-q", "-b", branch); err != nil {
			return "", err
		}
		return "created", nil
	}
	if _, err := git(local, "checkout", "-q", branch); err != nil {
		return "", err
	}
	return "switched", nil
}

func localBranchExists(local string, branch string) bool {
	_, err := git(local, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	return err == nil
}
