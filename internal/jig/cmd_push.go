package jig

import (
	"errors"
	"io"
	"strings"
)

type PushOptions struct {
	Path            string
	Id              string // selects one entry by identity instead of a path
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
	repos, err := selectInstalledRepos(ws, NodeQuery{Path: options.Path, Id: options.Id, IncludeArchived: options.IncludeArchived, Tags: options.Tags})
	if err != nil {
		return err
	}
	return runInRepos(out, repos, repoRun{Verb: "push", Label: "skipped"}, func(repo installedRepo) (string, string, error) {
		verb, err := pushRepo(repo.Local, options.SetUpstream)
		return verb, "", err
	})
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
