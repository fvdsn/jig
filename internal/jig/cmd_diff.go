package jig

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"
)

type DiffOptions struct {
	Path            string
	Id              string // selects one entry by identity instead of a path
	Stat            bool   // one summary line per dirty repository instead of the patch
	IncludeArchived bool
	Tags            []string
}

// Diff prints the uncommitted changes (staged and unstaged, against HEAD) of
// installed repositories as one workspace-wide unified diff: each
// repository's patch carries workspace-relative paths, so the output reads
// as the diff of the whole tree. Untracked files are status's business and
// do not appear, exactly as with git diff.
//
// On a terminal the patch reuses the user's git diff presentation: it is
// piped through the pager git would use (pager.diff, then GIT_PAGER /
// core.pager / PAGER, default less), and each repository's git runs with
// GIT_PAGER_IN_USE set so the user's color configuration applies exactly as
// when git itself pages. Piped output stays plain, like git.
func Diff(options DiffOptions, out io.Writer) error {
	ws, err := loadWorkspace(false)
	if err != nil {
		return err
	}
	selection, err := ws.Select(NodeQuery{Path: options.Path, Id: options.Id, IncludeArchived: options.IncludeArchived, Tags: options.Tags})
	if err != nil {
		return err
	}
	repos := selection.repoPaths()
	if len(repos) == 0 {
		return noEntriesMatchError(&ws.Model, "repositories", selection.Path, options.Tags)
	}

	onTerminal := false
	if f, ok := out.(*os.File); ok && !options.Stat && isTerminal(f) {
		onTerminal = true
	}

	type result struct {
		patch string
		stat  repoDiffStat
		err   error
	}
	results := make([]result, len(repos))
	forEachParallel(len(repos), func(i int) {
		abs := filepath.Join(ws.Root, repos[i])
		if !isGitRepo(abs) {
			return
		}
		if options.Stat {
			stat, err := repoDiffStats(abs, repos[i])
			results[i] = result{stat: stat, err: err}
			return
		}
		patch, err := repoDiffPatch(abs, repos[i], onTerminal)
		results[i] = result{patch: patch, err: err}
	})

	dst := out
	if onTerminal {
		if pagerIn, wait, err := startDiffPager(out.(*os.File)); err == nil && pagerIn != nil {
			dst = pagerIn
			defer wait()
		}
	}

	var stats []repoDiffStat
	var skipped []string
	for i, r := range results {
		if r.err != nil {
			skipped = append(skipped, fmt.Sprintf("%s: %s", repos[i], shortError(r.err)))
			continue
		}
		if options.Stat {
			if r.stat.files > 0 {
				stats = append(stats, r.stat)
			}
			continue
		}
		// A closed pager (the user quit) ends the output, not the command.
		if _, err := io.WriteString(dst, r.patch); err != nil {
			return nil
		}
	}
	printDiffStats(out, stats)
	printGroup(dst, "skipped", skipped)
	return nil
}

// repoDiffPatch renders the repository's diff against HEAD with the
// repository path folded into the a/ and b/ prefixes, so hunk headers carry
// workspace-relative paths. With pagerInUse the child git considers its
// output pager-bound, enabling the user's color configuration on auto.
func repoDiffPatch(abs string, repoPath string, pagerInUse bool) (string, error) {
	args := []string{"diff", "HEAD", "--src-prefix=a/" + repoPath + "/", "--dst-prefix=b/" + repoPath + "/"}
	if !pagerInUse {
		return git(abs, args...)
	}
	cmd := exec.Command("git", args...)
	cmd.Dir = abs
	cmd.Env = append(os.Environ(), "GIT_PAGER_IN_USE=true")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return stdout.String(), errors.New(msg)
	}
	return stdout.String(), nil
}

// startDiffPager launches the pager git diff would use, attached to the
// terminal, and returns the writer to feed plus a wait function. A disabled
// pager (pager.diff false, GIT_PAGER=cat) returns nil without error.
func startDiffPager(terminal *os.File) (io.WriteCloser, func(), error) {
	pager := gitDiffPagerCommand()
	if pager == "" {
		return nil, nil, nil
	}
	cmd := exec.Command(shellPath(), "-c", pager)
	cmd.Stdout = terminal
	cmd.Stderr = os.Stderr
	env := os.Environ()
	// The defaults git itself exports for its pager.
	if os.Getenv("LESS") == "" {
		env = append(env, "LESS=FRX")
	}
	if os.Getenv("LV") == "" {
		env = append(env, "LV=-c")
	}
	cmd.Env = env
	pagerIn, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}
	wait := func() {
		pagerIn.Close()
		_ = cmd.Wait()
	}
	return pagerIn, wait, nil
}

// gitDiffPagerCommand resolves the pager git diff would use: pager.diff
// first (false disables, true defers), then git var GIT_PAGER, which folds
// in GIT_PAGER, core.pager, PAGER, and the less default.
func gitDiffPagerCommand() string {
	if value, err := git("", "config", "--get", "pager.diff"); err == nil {
		value = strings.TrimSpace(value)
		if value == "false" {
			return ""
		}
		if value != "" && value != "true" {
			return value
		}
	}
	value, err := git("", "var", "GIT_PAGER")
	if err != nil {
		return ""
	}
	pager := strings.TrimSpace(value)
	if pager == "" || pager == "cat" {
		return ""
	}
	return pager
}

type repoDiffStat struct {
	path      string
	files     int
	additions int
	deletions int
}

func repoDiffStats(abs string, repoPath string) (repoDiffStat, error) {
	numstat, err := git(abs, "diff", "HEAD", "--numstat")
	if err != nil {
		return repoDiffStat{}, err
	}
	stat := repoDiffStat{path: repoPath}
	for _, line := range strings.Split(numstat, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		stat.files++
		// Binary files report "-" counts; they count as changed files only.
		if added, err := strconv.Atoi(fields[0]); err == nil {
			stat.additions += added
		}
		if deleted, err := strconv.Atoi(fields[1]); err == nil {
			stat.deletions += deleted
		}
	}
	return stat, nil
}

func printDiffStats(out io.Writer, stats []repoDiffStat) {
	maxPath := 0
	for _, stat := range stats {
		if w := utf8.RuneCountInString(stat.path); w > maxPath {
			maxPath = w
		}
	}
	for _, stat := range stats {
		noun := "files"
		if stat.files == 1 {
			noun = "file"
		}
		fmt.Fprintf(out, "%-*s  %d %s (+%d -%d)\n", maxPath, stat.path, stat.files, noun, stat.additions, stat.deletions)
	}
}
