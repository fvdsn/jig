package jig

import "io"

func Pull(out io.Writer) error {
	return runGitInInstalled(out, "pull", "pulled", "pull", "--ff-only")
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
