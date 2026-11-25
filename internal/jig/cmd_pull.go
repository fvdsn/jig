package jig

import (
	"io"
	"strings"
)

// Pull fast-forwards the installed repositories in the current scope.
func Pull(out io.Writer) error {
	ws, err := loadWorkspace(false)
	if err != nil {
		return err
	}
	repos, err := selectInstalledRepos(ws)
	if err != nil {
		return err
	}
	return runInRepos(out, repos, repoRun{Verb: "pull", Label: "skipped"}, func(repo installedRepo) (string, string, error) {
		return "pulled", "", pullRepo(repo.Local)
	})
}

// pullRepo is git pull --ff-only, except on a branch with no upstream and
// no commits of its own: that one follows the remote default branch, so
// the untouched repositories of a task stay current. A branch with no
// upstream and commits of its own fails as git pull does.
func pullRepo(local string) error {
	branch := gitBranch(local)
	if branch == "" || strings.HasPrefix(branch, "@") || branchHasUpstream(local, branch) {
		_, err := git(local, "pull", "--ff-only")
		return err
	}
	if _, err := git(local, "fetch", "origin"); err != nil {
		return err
	}
	if base, err := defaultBranch(local); err == nil {
		target := "refs/remotes/origin/" + base
		if _, err := git(local, "merge-base", "--is-ancestor", "HEAD", target); err == nil {
			_, err := git(local, "merge", "--ff-only", "--quiet", target)
			return err
		}
	}
	_, err := git(local, "pull", "--ff-only")
	return err
}

// branchHasUpstream reports whether a branch is configured to track
// another, whether or not that one still exists.
func branchHasUpstream(local string, branch string) bool {
	out, err := git(local, "config", "--get", "branch."+branch+".merge")
	return err == nil && strings.TrimSpace(out) != ""
}

// tipIsPublished reports whether a remote-tracking branch contains HEAD:
// the checkout has no commit that exists only locally.
func tipIsPublished(local string) bool {
	out, err := git(local, "for-each-ref", "--count=1", "--contains", "HEAD", "refs/remotes/")
	return err == nil && strings.TrimSpace(out) != ""
}

func Fetch(out io.Writer) error {
	return runGitInInstalled(out, "fetch", "fetched", "fetch")
}

// runGitInInstalled runs one git command across the installed repositories
// in the current scope, reporting each success with the given verb.
func runGitInInstalled(out io.Writer, command string, verb string, gitArgs ...string) error {
	ws, err := loadWorkspace(false)
	if err != nil {
		return err
	}
	repos, err := selectInstalledRepos(ws)
	if err != nil {
		return err
	}
	return runInRepos(out, repos, repoRun{Verb: command, Label: "skipped"}, func(repo installedRepo) (string, string, error) {
		_, err := git(repo.Local, gitArgs...)
		return verb, "", err
	})
}
