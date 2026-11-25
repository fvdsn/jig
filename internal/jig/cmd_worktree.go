package jig

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// A worktree is a second checkout of part of the schema tree, at
// worktrees/<dir>/, for one task: every repository in it is a git worktree
// of the root tree's clone, on the branch the worktree is named after. Jig
// keeps no record of it: a repository is in a worktree when a git checkout
// exists at its schema path there.

// worktreeSlash stands for "/" in a worktree's directory name, so a branch
// name maps to one directory and back.
const worktreeSlash = "__"

func worktreeDirName(name string) string {
	return strings.ReplaceAll(name, "/", worktreeSlash)
}

func worktreeBranchName(dir string) string {
	return strings.ReplaceAll(dir, worktreeSlash, "/")
}

// validateWorktreeName accepts the branch names a worktree can be named
// after.
func validateWorktreeName(name string) error {
	// --branch applies the stricter branch-name rules (e.g. no leading
	// dash), which also keeps a name from being parsed as a git option.
	if _, err := git("", "check-ref-format", "--branch", name); err != nil {
		return fmt.Errorf("invalid branch name %q", name)
	}
	if strings.Contains(name, worktreeSlash) {
		return fmt.Errorf("invalid worktree name %q: %q stands for \"/\" in the directory name", name, worktreeSlash)
	}
	return nil
}

// WorktreeCreate creates the worktree named after a branch with the
// repositories the selector matches and their dependencies, or adds them to
// it when it exists.
func WorktreeCreate(name string, options CloneOptions, out io.Writer) error {
	if err := validateWorktreeName(name); err != nil {
		return err
	}
	ws, err := loadWorkspace(true)
	if err != nil {
		return err
	}
	defer ws.Close()
	return addToWorktree(out, ws, name, options)
}

// addToWorktree installs the selection in the root tree where it is
// missing, then checks every repository of it out in the worktree, on the
// branch the worktree is named after. It releases the workspace lock once
// the root tree is settled: the rest goes through git, which locks for
// itself.
func addToWorktree(out io.Writer, ws *Workspace, name string, options CloneOptions) error {
	if err := validateWorktreeName(name); err != nil {
		return err
	}
	// The path is taken from the root of the tree the command runs in.
	if err := ws.requireTree(); err != nil {
		return err
	}
	dir := worktreeDirName(name)
	treeRoot := filepath.Join(ws.Root, worktreesDir, dir)

	selection, err := ws.Select(options.query())
	if err != nil {
		return err
	}
	roots := selection.repoPaths()
	if len(roots) == 0 {
		return noEntriesMatchError(&ws.Model, "repositories", selection.Path, options.Tags)
	}
	// The plan is judged against what the worktree holds, not the root
	// tree: a repository installed at the root for another task brings
	// nothing along here.
	present := map[string]bool{}
	for path, entry := range ws.Model.Entries {
		if entry.Kind == EntryRepo && isGitRepo(filepath.Join(treeRoot, filepath.FromSlash(path))) {
			present[entry.Identity] = true
		}
	}
	plan, err := resolvePlan(&ws.Model, roots, planOptions{
		IncludeOptional: options.IncludeOptional,
		IncludeArchived: options.IncludeArchived,
		IncludeRoots:    true,
		SkipDeps:        options.SkipDeps,
		Installed:       present,
	})
	if err != nil {
		return err
	}
	if len(plan.Repos) == 0 {
		return noEntriesMatchError(&ws.Model, "repositories", selection.Path, options.Tags)
	}

	// Root tree: jig clone, when something is missing there. Repositories
	// already installed are left alone, whatever their branch or state.
	installedBefore := map[string]bool{}
	for _, repoPath := range plan.Repos {
		_, installedBefore[repoPath] = installedPath(ws.Root, &ws.Model, &ws.State, repoPath)
	}
	var rootErr error
	if len(installedBefore) > 0 && !allTrue(installedBefore) {
		filtered := &lineFilter{out: out, drop: func(line string) bool { return strings.HasPrefix(line, "present") }}
		rootErr = resolveAndApplyPlan(filtered, ws, roots, nil, nil, applyOptions{
			IncludeOptional: options.IncludeOptional,
			IncludeArchived: options.IncludeArchived,
			SkipDeps:        options.SkipDeps,
		})
		filtered.flush()
		if err := saveState(ws.Root, ws.State); err != nil {
			return err
		}
	}
	repos := make([]installedRepo, len(plan.Repos))
	for i, repoPath := range plan.Repos {
		local, _ := installedPath(ws.Root, &ws.Model, &ws.State, repoPath)
		repos[i] = installedRepo{Path: repoPath, Local: local}
	}
	ws.Close()

	var mu sync.Mutex
	var notFetched []string
	addErr := runInRepos(out, repos, repoRun{Verb: "worktree", Label: "skipped"}, func(repo installedRepo) (string, string, error) {
		if repo.Local == "" {
			return "", "", errors.New("not installed in the root tree")
		}
		target := filepath.Join(treeRoot, filepath.FromSlash(repo.Path))
		// A clone made a moment ago has nothing to fetch.
		verb, fetchErr, err := addCheckout(repo.Local, target, name, installedBefore[repo.Path])
		if fetchErr != nil {
			mu.Lock()
			notFetched = append(notFetched, fmt.Sprintf("%s: %s", repo.Path, shortError(fetchErr)))
			mu.Unlock()
		}
		return verb, "", err
	})
	// The checkouts of a repository that could not be fetched start from
	// what its clone last fetched.
	printGroup(out, "not-fetched", notFetched)
	if pathExists(treeRoot) {
		fmt.Fprintf(out, "worktree: %s/%s\n", worktreesDir, dir)
	}
	if addErr != nil {
		return addErr
	}
	return rootErr
}

func allTrue(set map[string]bool) bool {
	for _, value := range set {
		if !value {
			return false
		}
	}
	return true
}

// addCheckout checks a repository out at target as a git worktree of its
// root clone, on the given branch, and returns the report verb. The branch
// is the local one when it exists, else one created from origin's and
// tracking it, else one created at the remote default branch without an
// upstream, so that it never pushes to the default branch. The start point
// never depends on what the root clone has checked out. A failed fetch is
// returned apart: the checkout is still made, from what the clone knows.
func addCheckout(local string, target string, branch string, fetch bool) (verb string, fetchErr error, err error) {
	if isGitRepo(target) {
		if main, ok := linkedWorktreeMain(target); ok && samePath(main, local) {
			return "present", nil, nil
		}
		return "", nil, errors.New("path exists and is not a git worktree of the root tree's clone")
	}
	if entries, readErr := os.ReadDir(target); readErr == nil && len(entries) > 0 {
		return "", nil, errors.New("path exists and is not empty")
	} else if readErr != nil && pathEntryExists(target) {
		return "", nil, errors.New("path exists and is not a directory")
	}
	if fetch {
		_, fetchErr = git(local, "fetch", "origin")
	}
	args := []string{"worktree", "add"}
	switch {
	case localBranchExists(local, branch):
		args = append(args, target, branch)
	case remoteBranchExists(local, branch):
		args = append(args, "--track", "-b", branch, target, "origin/"+branch)
	default:
		base, err := defaultBranch(local)
		if err != nil {
			return "", fetchErr, err
		}
		args = append(args, "--no-track", "-b", branch, target, "origin/"+base)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", fetchErr, err
	}
	if _, err := git(local, args...); err != nil {
		// A worktree whose directory was deleted by hand stays registered
		// at its path until pruned.
		if !strings.Contains(err.Error(), "registered") {
			return "", fetchErr, err
		}
		_, _ = git(local, "worktree", "prune")
		if _, err := git(local, args...); err != nil {
			return "", fetchErr, err
		}
	}
	return "added", fetchErr, nil
}

func remoteBranchExists(local string, branch string) bool {
	_, err := git(local, "rev-parse", "--verify", "--quiet", "refs/remotes/origin/"+branch)
	return err == nil
}

// linkedWorktreeMain returns the repository a git worktree hangs off, as
// the directory of its main checkout. ok is false for anything else: a
// clone of its own, a worktree whose repository is gone.
func linkedWorktreeMain(checkout string) (main string, ok bool) {
	info, err := os.Lstat(filepath.Join(checkout, ".git"))
	if err != nil || !info.Mode().IsRegular() {
		return "", false
	}
	out, err := git(checkout, "rev-parse", "--git-dir", "--git-common-dir")
	if err != nil {
		return "", false
	}
	dirs := strings.Split(strings.TrimSpace(out), "\n")
	if len(dirs) != 2 {
		return "", false
	}
	for i, dir := range dirs {
		if !filepath.IsAbs(dir) {
			dirs[i] = filepath.Join(checkout, dir)
		}
	}
	// A worktree's own git directory sits inside the repository's; a
	// repository checked out in place has just the one.
	if samePath(dirs[0], dirs[1]) || filepath.Base(dirs[1]) != ".git" {
		return "", false
	}
	return filepath.Dir(dirs[1]), true
}

// lineFilter passes whole lines through to out, except the ones drop
// names.
type lineFilter struct {
	out  io.Writer
	drop func(line string) bool
	buf  []byte
}

func (f *lineFilter) Write(p []byte) (int, error) {
	f.buf = append(f.buf, p...)
	for {
		end := bytes.IndexByte(f.buf, '\n')
		if end < 0 {
			return len(p), nil
		}
		line := f.buf[:end+1]
		if !f.drop(string(line)) {
			if _, err := f.out.Write(line); err != nil {
				return 0, err
			}
		}
		f.buf = f.buf[end+1:]
	}
}

func (f *lineFilter) flush() {
	if len(f.buf) > 0 && !f.drop(string(f.buf)) {
		_, _ = f.out.Write(f.buf)
	}
	f.buf = nil
}

type WorktreeRemoveOptions struct {
	Name  string // the branch the worktree is named after, or its directory name
	Force bool
}

// WorktreeRemove removes every git checkout found under the worktree's
// directory, then the directory. Checkouts are found by walking it, so one
// whose schema path has changed since is still removed. Commits on a branch
// live in the root tree's clone and survive; a checkout is refused only
// when removing it would lose work.
func WorktreeRemove(options WorktreeRemoveOptions, out io.Writer) error {
	dir := worktreeDirName(options.Name)
	if dir == "" || dir == "." || dir == ".." || strings.ContainsAny(dir, `\`) {
		return fmt.Errorf("invalid worktree name %q", options.Name)
	}
	ws, err := loadWorkspace(false)
	if err != nil {
		return err
	}
	treeRoot := filepath.Join(ws.Root, worktreesDir, dir)
	if info, err := os.Stat(treeRoot); err != nil || !info.IsDir() {
		return fmt.Errorf("no worktree %s: %s/%s does not exist", options.Name, worktreesDir, dir)
	}
	if ws.Worktree == dir {
		return fmt.Errorf("the current directory is inside worktree %s; leave it first", options.Name)
	}

	var checkouts, other []string
	_, loose := scanWorktree(treeRoot, "", &checkouts)
	other = append(other, loose...)

	type result struct {
		kept []string
		err  error
	}
	results := make([]result, len(checkouts))
	tracker := newProgress(len(checkouts))
	forEachParallel(len(checkouts), func(i int) {
		tracker.start(checkouts[i])
		kept, err := removeCheckout(filepath.Join(treeRoot, filepath.FromSlash(checkouts[i])), worktreeBranchName(dir), options.Force)
		tracker.finish(checkouts[i])
		results[i] = result{kept, err}
	})
	tracker.close()

	var failures []string
	for i, r := range results {
		if r.err != nil {
			failures = append(failures, fmt.Sprintf("%s: %s", checkouts[i], r.err))
			continue
		}
		fmt.Fprintf(out, "removed: %s\n", checkouts[i])
		for _, branch := range r.kept {
			fmt.Fprintf(out, "kept branch: %s %s\n", checkouts[i], branch)
		}
	}
	if options.Force {
		var remaining []string
		for _, rel := range other {
			if err := os.RemoveAll(filepath.Join(treeRoot, filepath.FromSlash(rel))); err != nil {
				remaining = append(remaining, rel)
			}
		}
		other = remaining
	}
	removeEmptyDirs(treeRoot)
	_ = os.Remove(filepath.Join(ws.Root, worktreesDir))

	printGroup(out, "not-removed", failures)
	printGroup(out, "kept", other)
	switch {
	case len(failures) > 0:
		return fmt.Errorf("%d checkouts not removed", len(failures))
	case len(other) > 0:
		return fmt.Errorf("%s/%s holds other content, which was kept (use --force to delete it)", worktreesDir, dir)
	}
	return nil
}

// scanWorktree walks a worktree's directory for its git worktrees, without
// entering them, and returns what else lies there: files, and directories
// that hold files but no worktree, named once. A git repository that is
// not a worktree is other content too.
func scanWorktree(abs string, rel string, checkouts *[]string) (hasCheckout bool, loose []string) {
	entries, err := os.ReadDir(abs)
	if err != nil {
		return false, nil
	}
	for _, entry := range entries {
		childAbs := filepath.Join(abs, entry.Name())
		childRel := entry.Name()
		if rel != "" {
			childRel = rel + "/" + entry.Name()
		}
		if !entry.IsDir() {
			loose = append(loose, childRel)
			continue
		}
		if pathEntryExists(filepath.Join(childAbs, ".git")) {
			if _, ok := linkedWorktreeMain(childAbs); ok {
				*checkouts = append(*checkouts, childRel)
				hasCheckout = true
			} else {
				loose = append(loose, childRel+"/")
			}
			continue
		}
		childHas, childLoose := scanWorktree(childAbs, childRel, checkouts)
		switch {
		case childHas:
			hasCheckout = true
			loose = append(loose, childLoose...)
		case len(childLoose) > 0:
			loose = append(loose, childRel+"/")
		}
	}
	return hasCheckout, loose
}

// removeEmptyDirs deletes dir when nothing but empty directories is left
// in it.
func removeEmptyDirs(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() {
			removeEmptyDirs(filepath.Join(dir, entry.Name()))
		}
	}
	_ = os.Remove(dir)
}

// removeCheckout removes one git worktree with git worktree remove, so the
// repository's worktree list stays accurate. Unless forced it refuses a
// checkout with uncommitted changes, untracked files included, or with
// commits no branch holds. It then deletes the worktree's branch and the
// one the checkout was on from the repository when a remote-tracking
// branch contains them, and returns the ones it kept, described.
func removeCheckout(checkout string, name string, force bool) (keptBranches []string, err error) {
	main, ok := linkedWorktreeMain(checkout)
	if !ok {
		return nil, errors.New("not a git worktree")
	}
	if !force {
		if isDirty(checkout) {
			return nil, forceHint(errors.New("uncommitted changes"))
		}
		if !headOnABranch(checkout) {
			return nil, forceHint(errors.New("commits on a detached HEAD that no branch contains"))
		}
	}
	current := ""
	if out, err := git(checkout, "branch", "--show-current"); err == nil {
		current = strings.TrimSpace(out)
	}
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	if _, err := git(main, append(args, checkout)...); err != nil {
		return nil, errors.New(shortError(err))
	}

	base := ""
	if out, err := git(main, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil {
		base = strings.TrimPrefix(strings.TrimSpace(out), "origin/")
	}
	inUse := checkedOutBranches(main)
	branches := []string{name}
	if current != "" && current != name {
		branches = append(branches, current)
	}
	for _, branch := range branches {
		// The default branch is the root tree's, and a branch another
		// tree has checked out is that tree's.
		if branch == base || inUse[branch] || !localBranchExists(main, branch) {
			continue
		}
		ref := "refs/heads/" + branch
		if published, err := git(main, "for-each-ref", "--count=1", "--contains", ref, "refs/remotes/"); err == nil && strings.TrimSpace(published) != "" {
			_, _ = git(main, "branch", "-D", branch)
			continue
		}
		count := "?"
		if out, err := git(main, "rev-list", "--count", ref, "--not", "--remotes"); err == nil {
			count = strings.TrimSpace(out)
		}
		noun := "unpublished commits"
		if count == "1" {
			noun = "unpublished commit"
		}
		keptBranches = append(keptBranches, fmt.Sprintf("(%s, %s %s)", branch, count, noun))
	}
	return keptBranches, nil
}

// headOnABranch reports whether a local or remote-tracking branch contains
// HEAD: when none does, removing the checkout leaves its commits
// unreachable.
func headOnABranch(checkout string) bool {
	out, err := git(checkout, "for-each-ref", "--count=1", "--contains", "HEAD", "refs/heads/", "refs/remotes/")
	return err != nil || strings.TrimSpace(out) != ""
}

// checkedOutBranches returns the branches the repository's checkouts are
// on, the main one and its worktrees.
func checkedOutBranches(main string) map[string]bool {
	branches := map[string]bool{}
	out, err := git(main, "worktree", "list", "--porcelain")
	if err != nil {
		return branches
	}
	for _, line := range strings.Split(out, "\n") {
		if branch, ok := strings.CutPrefix(line, "branch refs/heads/"); ok {
			branches[branch] = true
		}
	}
	return branches
}

// removeFromWorktree is jig rm inside a worktree: it removes the checkout
// of one repository from it, under the rules of WorktreeRemove.
func removeFromWorktree(out io.Writer, ws *Workspace, entry Entry, force bool) error {
	local, ok := ws.checkout(entry.Path)
	if !ok {
		return errors.New("not in this worktree")
	}
	kept, err := removeCheckout(local, worktreeBranchName(ws.Worktree), force)
	if err != nil {
		return err
	}
	pruneEmptyParents(ws.TreeRoot(), filepath.Dir(filepath.FromSlash(entry.Path)))
	fmt.Fprintf(out, "removed: %s\n", entry.Path)
	sort.Strings(kept)
	for _, branch := range kept {
		fmt.Fprintf(out, "kept branch: %s %s\n", entry.Path, branch)
	}
	return nil
}
