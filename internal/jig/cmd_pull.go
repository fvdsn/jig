package jig

import "io"

type PullOptions struct {
	Selector
}

func Pull(options PullOptions, out io.Writer) error {
	return runGitInInstalled(out, options.query(), "pull", "pulled", "pull", "--ff-only")
}

type FetchOptions struct {
	Selector
}

func Fetch(options FetchOptions, out io.Writer) error {
	return runGitInInstalled(out, options.query(), "fetch", "fetched", "fetch")
}

// runGitInInstalled runs one git command across the installed repositories
// matching the query, reporting each success with the given verb.
func runGitInInstalled(out io.Writer, query NodeQuery, command string, verb string, gitArgs ...string) error {
	ws, err := loadWorkspace(false)
	if err != nil {
		return err
	}
	repos, err := selectInstalledRepos(ws, query)
	if err != nil {
		return err
	}
	return runInRepos(out, repos, repoRun{Verb: command, Label: "skipped"}, func(repo installedRepo) (string, string, error) {
		_, err := git(repo.Local, gitArgs...)
		return verb, "", err
	})
}
