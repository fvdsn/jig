package jig

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
)

// Git runs one git command, verbatim, in every installed repository under
// the current directory. It is raw git: none of the safety the curated verbs
// add (ff-only pulls, never-forced pushes, checkouts that skip dirty repos)
// applies, so `jig git reset --hard` does exactly what it says in every
// repository in scope. Output is captured, so git runs non-interactively:
// credential prompts fail instead of hanging, and editor-opening commands
// are out of scope.
//
// Repositories run in parallel and report in path order, each as "<path>:"
// followed by its output with continuation lines indented, like the failure
// groups of the other verbs; a repository that prints nothing reports "done".
// Failed repositories are listed in a failed group at the end, with their
// exit status, and fail the command.
func Git(args []string, out io.Writer) error {
	if len(args) == 0 {
		return errors.New("jig git needs a git command to run")
	}
	ws, err := loadWorkspace(false)
	if err != nil {
		return err
	}
	repos, err := selectInstalledRepos(ws)
	if err != nil {
		return err
	}
	if len(repos) == 0 {
		return errors.New("no installed repositories in scope")
	}
	if f, ok := out.(*os.File); ok && isTerminal(f) {
		// Git sees a pipe and turns color off; restore the user's choice
		// when the output does reach a terminal.
		args = append([]string{"-c", "color.ui=always"}, args...)
	}

	type result struct {
		output string
		err    error
	}
	results := make([]*result, len(repos))
	var mu sync.Mutex
	next := 0 // results[next] is the first repository not yet reported
	var failed []string
	tracker := newProgress(len(repos))
	forEachParallel(len(repos), func(i int) {
		tracker.start(repos[i].Path)
		output, err := gitVerbatim(repos[i].Local, args)
		tracker.finish(repos[i].Path)
		mu.Lock()
		defer mu.Unlock()
		results[i] = &result{output, err}
		// Report in path order: flush every completed repository that
		// follows the last reported one.
		for next < len(repos) && results[next] != nil {
			repo, r := repos[next], results[next]
			next++
			body := strings.TrimSpace(r.output)
			if body == "" {
				body = "done"
			}
			tracker.println(out, fmt.Sprintf("%s: %s", repo.Path, strings.ReplaceAll(body, "\n", "\n  ")))
			if r.err != nil {
				failed = append(failed, fmt.Sprintf("%s: %s", repo.Path, r.err))
			}
		}
	})
	tracker.close()
	printGroup(out, "failed", failed)
	if len(failed) > 0 {
		return fmt.Errorf("git: %d repositories failed", len(failed))
	}
	return nil
}

// gitVerbatim runs git with the arguments as given, in dir, returning its
// combined output in the order git produced it.
func gitVerbatim(dir string, args []string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	output, err := cmd.CombinedOutput()
	return string(output), err
}
