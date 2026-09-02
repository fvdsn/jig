package jig

import (
	"fmt"
	"io"
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
		patch, err := repoDiffPatch(abs, repos[i])
		results[i] = result{patch: patch, err: err}
	})

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
		io.WriteString(out, r.patch)
	}
	printDiffStats(out, stats)
	printGroup(out, "skipped", skipped)
	return nil
}

// repoDiffPatch renders the repository's diff against HEAD with the
// repository path folded into the a/ and b/ prefixes, so hunk headers carry
// workspace-relative paths.
func repoDiffPatch(abs string, repoPath string) (string, error) {
	return git(abs, "diff", "HEAD", "--src-prefix=a/"+repoPath+"/", "--dst-prefix=b/"+repoPath+"/")
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
