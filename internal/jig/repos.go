package jig

import (
	"fmt"
	"io"
	"strings"
	"sync"
)

// installedRepo is a selected repository that is checked out on disk.
type installedRepo struct {
	Path  string // schema path
	Local string // absolute checkout path (the recorded path when a move is pending)
}

// selectInstalledRepos resolves a query to the matching repositories that
// are installed, in schema order.
func selectInstalledRepos(ws *Workspace, query NodeQuery) ([]installedRepo, error) {
	selection, err := ws.Select(query)
	if err != nil {
		return nil, err
	}
	var repos []installedRepo
	for _, entry := range selection.ofKind(EntryRepo) {
		if local, ok := installedPath(ws.Root, &ws.Model, &ws.State, entry.Path); ok {
			repos = append(repos, installedRepo{entry.Path, local})
		}
	}
	return repos, nil
}

// repoRun describes one command's pass over repositories.
type repoRun struct {
	Verb  string   // the command, for the failure summary ("pull", "setup")
	Label string   // how a failed repository is reported: "skipped" or "failed"
	Order []string // run sequentially in this path order; nil runs in parallel
}

// runInRepos runs task over every repository, in parallel unless an order
// is given. Each repository reports one line as it completes, "<verb>:
// <path> <note>" on success and "<label>: <path>" on failure, with a
// transient progress line on a terminal; the failures follow in a <label>
// group with their messages, and any failure fails the command.
func runInRepos(out io.Writer, repos []installedRepo, run repoRun, task func(repo installedRepo) (verb string, note string, err error)) error {
	var mu sync.Mutex
	var failed []string
	tracker := newProgress(len(repos))
	runOne := func(repo installedRepo) {
		tracker.start(repo.Path)
		verb, note, err := task(repo)
		tracker.finish(repo.Path)
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			msg := strings.ReplaceAll(strings.TrimSpace(err.Error()), "\n", "\n  ")
			failed = append(failed, fmt.Sprintf("%s: %s", repo.Path, msg))
			tracker.println(out, fmt.Sprintf("%s: %s", run.Label, repo.Path))
			return
		}
		line := fmt.Sprintf("%s: %s", verb, repo.Path)
		if note != "" {
			line += " " + note
		}
		tracker.println(out, line)
	}
	if run.Order != nil {
		byPath := map[string]installedRepo{}
		for _, repo := range repos {
			byPath[repo.Path] = repo
		}
		for _, path := range run.Order {
			runOne(byPath[path])
		}
	} else {
		forEachParallel(len(repos), func(i int) { runOne(repos[i]) })
	}
	tracker.close()
	printGroup(out, run.Label, failed)
	if len(failed) > 0 {
		return fmt.Errorf("%s: %d repositories %s", run.Verb, len(failed), run.Label)
	}
	return nil
}
