package jig

import "io"

type PullOptions struct {
	Path            string
	Id              string // selects one entry by identity instead of a path
	IncludeArchived bool
	Tags            []string
}

func Pull(options PullOptions, out io.Writer) error {
	return runGitInInstalled(out, NodeQuery{Path: options.Path, Id: options.Id, IncludeArchived: options.IncludeArchived, Tags: options.Tags}, "pull", "pulled", "pull", "--ff-only")
}

type FetchOptions struct {
	Path            string
	Id              string // selects one entry by identity instead of a path
	IncludeArchived bool
	Tags            []string
}

func Fetch(options FetchOptions, out io.Writer) error {
	return runGitInInstalled(out, NodeQuery{Path: options.Path, Id: options.Id, IncludeArchived: options.IncludeArchived, Tags: options.Tags}, "fetch", "fetched", "fetch")
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
