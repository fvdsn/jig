package jig

import (
	"errors"
	"fmt"
	"io"
	"strings"
)

type SwitchOptions struct {
	Branches []string // candidates in order; the first that exists in a repository is taken
	Create   bool     // create the (single) branch when it does not exist
	Default  bool     // fall back to each repository's remote default branch
}

// Switch switches the installed repositories in the current scope to the
// first listed branch each of them has, or to its remote's default branch,
// mirroring git switch across the workspace. Safety is git's own: a plain
// (non-forced) switch never discards local changes, so repositories where
// the switch would lose work fail and are reported as skipped.
func Switch(options SwitchOptions, out io.Writer) error {
	// --branch applies the stricter branch-name rules (e.g. no leading
	// dash), which also keeps a name from being parsed as a git option.
	for _, branch := range options.Branches {
		if _, err := git("", "check-ref-format", "--branch", branch); err != nil {
			return fmt.Errorf("invalid branch name %q", branch)
		}
	}
	if options.Create && len(options.Branches) != 1 {
		return errors.New("-c creates one branch; give exactly one name")
	}
	if len(options.Branches) == 0 && !options.Default {
		return errors.New("no branch given")
	}
	ws, err := loadWorkspace(false)
	if err != nil {
		return err
	}
	return switchWorkspace(ws, options, out)
}

// switchWorkspace is Switch on a loaded workspace, for init --switch.
func switchWorkspace(ws *Workspace, options SwitchOptions, out io.Writer) error {
	repos, err := selectInstalledRepos(ws)
	if err != nil {
		return err
	}
	// When the target can differ per repository, the report names it.
	named := len(options.Branches) > 1 || options.Default
	return runInRepos(out, repos, repoRun{Verb: "switch", Label: "skipped"}, func(repo installedRepo) (string, string, error) {
		if options.Create {
			verb, err := checkoutRepo(repo.Local, options.Branches[0], true)
			return verb, "", err
		}
		branch := ""
		for _, candidate := range options.Branches {
			if branchExists(repo.Local, candidate) {
				branch = candidate
				break
			}
		}
		if branch == "" {
			if !options.Default {
				return "", "", fmt.Errorf("none of %s exists", strings.Join(options.Branches, ", "))
			}
			resolved, err := defaultBranch(repo.Local)
			if err != nil {
				return "", "", err
			}
			branch = resolved
		}
		note := ""
		if named {
			note = "(" + branch + ")"
		}
		verb, err := checkoutRepo(repo.Local, branch, false)
		return verb, note, err
	})
}

// checkoutRepo switches one repository and returns the report verb. Creating
// is idempotent: when the branch already exists, locally or on origin, -c
// degrades to a plain switch instead of failing or forking a second branch.
func checkoutRepo(local string, branch string, create bool) (string, error) {
	if gitBranch(local) == branch {
		return "present", nil
	}
	if create && !branchExists(local, branch) {
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

// defaultBranch resolves the default branch of one repository's origin.
// origin/HEAD answers offline and is set at clone time; when it is missing
// the remote is asked directly, and the answer recorded with set-head so the
// next call is offline again.
func defaultBranch(local string) (string, error) {
	if out, err := git(local, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil {
		return strings.TrimPrefix(strings.TrimSpace(out), "origin/"), nil
	}
	out, err := git(local, "ls-remote", "--symref", "origin", "HEAD")
	if err != nil {
		return "", fmt.Errorf("could not resolve the default branch: %s", shortError(err))
	}
	for _, line := range strings.Split(out, "\n") {
		// The symref line reads "ref: refs/heads/<branch>\tHEAD".
		fields := strings.Fields(line)
		if len(fields) == 3 && fields[0] == "ref:" && fields[2] == "HEAD" {
			if branch, ok := strings.CutPrefix(fields[1], "refs/heads/"); ok {
				_, _ = git(local, "remote", "set-head", "origin", branch)
				return branch, nil
			}
		}
	}
	return "", fmt.Errorf("origin did not report a default branch")
}

func localBranchExists(local string, branch string) bool {
	_, err := git(local, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	return err == nil
}

// branchExists reports whether a repository has the branch locally or as
// a remote-tracking branch of origin, from which git creates the local
// one on switch.
func branchExists(local string, branch string) bool {
	if localBranchExists(local, branch) {
		return true
	}
	_, err := git(local, "rev-parse", "--verify", "--quiet", "refs/remotes/origin/"+branch)
	return err == nil
}
