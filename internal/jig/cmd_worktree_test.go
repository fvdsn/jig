package jig

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// worktreeFixture is a workspace over three pushable remotes: services/a
// depends on platform/b, and platform/c stands alone. Nothing is installed.
type worktreeFixture struct {
	t       *testing.T
	root    string
	remotes map[string]string
}

func newWorktreeFixture(t *testing.T) *worktreeFixture {
	t.Helper()
	// Resolved, since git reports resolved paths.
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := &worktreeFixture{t: t, root: filepath.Join(root, "ws"), remotes: map[string]string{}}
	for _, name := range []string{"a", "b", "c"} {
		f.remotes[name] = testBareRemote(t, root, "remote-"+name)
	}
	writeTestWorkspace(t, f.root, fmt.Sprintf(`{
  "version": 3,
  "tree": {
    "services/a": { "$repo": { "id": "a", "git": %q, "dependsOn": [ { "path": "platform/b" } ] } },
    "platform/b": { "$repo": { "id": "b", "git": %q } },
    "platform/c": { "$repo": { "id": "c", "git": %q } }
  }
}`, f.remotes["a"], f.remotes["b"], f.remotes["c"]))
	t.Chdir(f.root)
	return f
}

func (f *worktreeFixture) path(parts ...string) string {
	return filepath.Join(append([]string{f.root}, parts...)...)
}

// run calls a command and returns its output, failing the test on error.
func (f *worktreeFixture) run(command func(out *bytes.Buffer) error) string {
	f.t.Helper()
	var out bytes.Buffer
	if err := command(&out); err != nil {
		f.t.Fatalf("%v\n%s", err, out.String())
	}
	return out.String()
}

// refused calls a command that must fail and returns its output and error.
func (f *worktreeFixture) refused(command func(out *bytes.Buffer) error) string {
	f.t.Helper()
	var out bytes.Buffer
	err := command(&out)
	if err == nil {
		f.t.Fatalf("expected an error, got:\n%s", out.String())
	}
	return out.String() + "error: " + err.Error()
}

func (f *worktreeFixture) create(name string, path string) string {
	f.t.Helper()
	return f.run(func(out *bytes.Buffer) error {
		return WorktreeCreate(name, CloneOptions{Selector: Selector{Path: path}}, out)
	})
}

func (f *worktreeFixture) remove(name string, force bool) string {
	f.t.Helper()
	return f.run(func(out *bytes.Buffer) error {
		return WorktreeRemove(WorktreeRemoveOptions{Name: name, Force: force}, out)
	})
}

// commit adds a file to a checkout and commits it.
func (f *worktreeFixture) commit(checkout string, file string) {
	f.t.Helper()
	if err := os.WriteFile(filepath.Join(checkout, file), []byte(file+"\n"), 0o644); err != nil {
		f.t.Fatal(err)
	}
	gitIn(f.t, checkout, "add", ".")
	gitIn(f.t, checkout, "commit", "-qm", file)
}

func (f *worktreeFixture) head(dir string, rev string) string {
	f.t.Helper()
	out, err := git(dir, "rev-parse", rev)
	if err != nil {
		f.t.Fatalf("rev-parse %s in %s: %v", rev, dir, err)
	}
	return strings.TrimSpace(out)
}

func wantLines(t *testing.T, got string, lines ...string) {
	t.Helper()
	for _, line := range lines {
		if !strings.Contains(got, line) {
			t.Fatalf("missing %q in:\n%s", line, got)
		}
	}
}

func TestWorktreeCreate(t *testing.T) {
	f := newWorktreeFixture(t)

	// The root clone of b is in the way a task leaves it: on another
	// branch, behind its remote, with uncommitted changes.
	f.run(func(out *bytes.Buffer) error {
		return Clone(CloneOptions{Selector: Selector{Path: "platform/b"}}, out)
	})
	rootB := f.path("platform", "b")
	other := f.path("..", "other-b")
	gitIn(t, f.root, "clone", "-q", f.remotes["b"], other)
	f.commit(other, "upstream.txt")
	gitIn(t, other, "push", "-q")
	gitIn(t, rootB, "switch", "-q", "-c", "someone-elses")
	f.commit(rootB, "local.txt")
	if err := os.WriteFile(filepath.Join(rootB, "wip.txt"), []byte("wip\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rootHead := f.head(rootB, "HEAD")

	// A missing repository is cloned at the root, the installed one is
	// left alone, and both are checked out on the branch, dependency
	// included, at the remote default branch as it is now.
	got := f.create("PLT-1", "services/a")
	wantLines(t, got, "cloned: services/a\n", "added: services/a\n", "added: platform/b\n", "worktree: worktrees/PLT-1\n")
	if strings.Contains(got, "present") {
		t.Fatalf("the root tree's present repositories should not be reported:\n%s", got)
	}
	for _, repo := range []string{"services/a", "platform/b"} {
		checkout := f.path("worktrees", "PLT-1", repo)
		if branch := gitBranch(checkout); branch != "PLT-1" {
			t.Fatalf("%s is on %q, want PLT-1", repo, branch)
		}
		if branchHasUpstream(checkout, "PLT-1") {
			t.Fatalf("%s: the new branch tracks the default branch", repo)
		}
	}
	if !pathExists(f.path("worktrees", "PLT-1", "platform", "b", "upstream.txt")) {
		t.Fatal("the checkout does not start from the fetched default branch")
	}
	if pathExists(f.path("worktrees", "PLT-1", "platform", "b", "local.txt")) {
		t.Fatal("the checkout starts from what the root clone has checked out")
	}
	if gitBranch(rootB) != "someone-elses" || f.head(rootB, "HEAD") != rootHead || !pathExists(filepath.Join(rootB, "wip.txt")) {
		t.Fatal("the root clone was touched")
	}
	if gitBranch(f.path("services", "a")) == "PLT-1" {
		t.Fatal("the root tree moved to the worktree's branch")
	}
	if pathExists(f.path("worktrees", "PLT-1", "platform", "c")) || pathExists(f.path("platform", "c")) {
		t.Fatal("an unrelated repository was installed")
	}
	state, err := loadState(f.root)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := state.Repos["a"]; !ok {
		t.Fatal("the repository cloned at the root is not tracked")
	}

	// Running it again changes nothing.
	got = f.create("PLT-1", "services/a")
	wantLines(t, got, "present: services/a\n", "present: platform/b\n")
	if strings.Contains(got, "added") || strings.Contains(got, "cloned") {
		t.Fatalf("expected a no-op:\n%s", got)
	}

	// Another path extends the worktree.
	got = f.create("PLT-1", "platform")
	wantLines(t, got, "cloned: platform/c\n", "added: platform/c\n", "present: platform/b\n")

	// The root tree's status knows nothing of worktrees.
	got = f.run(func(out *bytes.Buffer) error { return Status(StatusOptions{}, out) })
	if strings.Contains(got, "worktrees") || strings.Contains(got, "PLT-1") {
		t.Fatalf("the root status reaches into worktrees:\n%s", got)
	}
}

func TestWorktreeCreateChoosesTheBranch(t *testing.T) {
	f := newWorktreeFixture(t)
	f.run(func(out *bytes.Buffer) error { return Clone(CloneOptions{}, out) })

	// a: the branch exists locally, with a commit.
	rootA := f.path("services", "a")
	gitIn(t, rootA, "branch", "task")
	gitIn(t, rootA, "switch", "-q", "task")
	f.commit(rootA, "local.txt")
	localTip := f.head(rootA, "HEAD")
	gitIn(t, rootA, "switch", "-q", "-")
	// b: the branch exists on the remote only, pushed by someone else
	// since the root clone last fetched.
	other := f.path("..", "other-b")
	gitIn(t, f.root, "clone", "-q", f.remotes["b"], other)
	gitIn(t, other, "switch", "-q", "-c", "task")
	f.commit(other, "remote.txt")
	gitIn(t, other, "push", "-q", "-u", "origin", "task")
	// c: the root tree has the branch checked out.
	gitIn(t, f.path("platform", "c"), "switch", "-q", "-c", "task")

	got := f.refused(func(out *bytes.Buffer) error {
		return WorktreeCreate("task", CloneOptions{Selector: Selector{Path: "."}}, out)
	})
	wantLines(t, got, "added: services/a\n", "added: platform/b\n", "skipped: platform/c\n", "worktree: worktrees/task\n")
	if !strings.Contains(got, "platform/c: ") || !strings.Contains(got, "already") {
		t.Fatalf("expected the skip to say the branch is in use:\n%s", got)
	}
	if head := f.head(f.path("worktrees", "task", "services", "a"), "HEAD"); head != localTip {
		t.Fatal("the existing local branch was not reused")
	}
	checkoutB := f.path("worktrees", "task", "platform", "b")
	if !pathExists(filepath.Join(checkoutB, "remote.txt")) {
		t.Fatal("the branch was not created from origin's")
	}
	if !branchHasUpstream(checkoutB, "task") {
		t.Fatal("the branch created from origin's does not track it")
	}
	if pathExists(f.path("worktrees", "task", "platform", "c")) {
		t.Fatal("the skipped repository left a directory")
	}
}

func TestWorktreeNames(t *testing.T) {
	f := newWorktreeFixture(t)

	got := f.create("feature/CX-12", "platform/c")
	wantLines(t, got, "worktree: worktrees/feature__CX-12\n")
	checkout := f.path("worktrees", "feature__CX-12", "platform", "c")
	if branch := gitBranch(checkout); branch != "feature/CX-12" {
		t.Fatalf("branch = %q, want feature/CX-12", branch)
	}

	for _, name := range []string{"a__b", "-x", "a..b", ""} {
		var out bytes.Buffer
		if err := WorktreeCreate(name, CloneOptions{Selector: Selector{Path: "platform/c"}}, &out); err == nil {
			t.Fatalf("expected name %q to be refused", name)
		}
	}
	if pathExists(f.path("worktrees", "a__b")) {
		t.Fatal("a refused name left a directory")
	}

	// jig clone inside a worktree adds to it, on its branch.
	t.Chdir(f.path("worktrees", "feature__CX-12"))
	got = f.run(func(out *bytes.Buffer) error {
		return Clone(CloneOptions{Selector: Selector{Path: "platform/b"}}, out)
	})
	wantLines(t, got, "cloned: platform/b\n", "added: platform/b\n")
	if branch := gitBranch(f.path("worktrees", "feature__CX-12", "platform", "b")); branch != "feature/CX-12" {
		t.Fatalf("branch = %q, want feature/CX-12", branch)
	}
	t.Chdir(f.root)

	// Remove takes the branch name or the directory name.
	f.remove("feature/CX-12", false)
	if pathExists(f.path("worktrees")) {
		t.Fatal("the worktree directory was not removed")
	}
	f.create("feature/CX-12", "platform/c")
	f.remove("feature__CX-12", false)
	if pathExists(f.path("worktrees")) {
		t.Fatal("the worktree directory was not removed by its directory name")
	}
	got = f.refused(func(out *bytes.Buffer) error {
		return WorktreeRemove(WorktreeRemoveOptions{Name: "nope"}, out)
	})
	wantLines(t, got, "no worktree nope")
}

func TestWorktreeRemoveKeepsWork(t *testing.T) {
	f := newWorktreeFixture(t)
	f.create("task", ".")
	tree := f.path("worktrees", "task")
	a, b, c := filepath.Join(tree, "services", "a"), filepath.Join(tree, "platform", "b"), filepath.Join(tree, "platform", "c")

	// a has uncommitted work, b has commits no branch holds, c is
	// untouched.
	if err := os.WriteFile(filepath.Join(a, "wip.txt"), []byte("wip\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, b, "switch", "-q", "--detach")
	f.commit(b, "detached.txt")
	got := f.refused(func(out *bytes.Buffer) error {
		return WorktreeRemove(WorktreeRemoveOptions{Name: "task"}, out)
	})
	wantLines(t, got, "removed: platform/c\n", "services/a: uncommitted changes (use --force)", "platform/b: commits on a detached HEAD that no branch contains (use --force)")
	if !pathExists(filepath.Join(a, "wip.txt")) || !pathExists(filepath.Join(b, "detached.txt")) {
		t.Fatal("a refused checkout was removed")
	}
	if pathExists(c) {
		t.Fatal("the untouched checkout was kept")
	}
	if localBranchExists(f.path("platform", "c"), "task") {
		t.Fatal("the untouched branch was kept in the root clone")
	}

	// Committed on a branch, the work lives in the root clone: the
	// checkout goes, the branches stay and are reported. A pushed branch
	// goes with its checkout.
	gitIn(t, a, "add", ".")
	gitIn(t, a, "commit", "-qm", "wip")
	gitIn(t, b, "switch", "-q", "-c", "task-second-mr")
	gitIn(t, b, "push", "-q", "-u", "origin", "task-second-mr")
	got = f.remove("task", false)
	wantLines(t, got, "removed: services/a\n", "removed: platform/b\n", "kept branch: services/a (task, 1 unpublished commit)\n")
	if strings.Contains(got, "kept branch: platform/b") {
		t.Fatalf("published and untouched branches should be deleted:\n%s", got)
	}
	if !localBranchExists(f.path("services", "a"), "task") {
		t.Fatal("the branch with unpublished commits was deleted")
	}
	rootB := f.path("platform", "b")
	if localBranchExists(rootB, "task") || localBranchExists(rootB, "task-second-mr") {
		t.Fatal("the untouched and the pushed branch were kept")
	}
	if pathExists(tree) {
		t.Fatal("the worktree directory was not removed")
	}
	if out, _ := git(f.path("services", "a"), "worktree", "list"); strings.Contains(out, "worktrees/task") {
		t.Fatalf("the root clone still lists the worktree:\n%s", out)
	}

	// The kept branch is reused, with its commit, by the next create.
	f.create("task", "services/a")
	if !pathExists(filepath.Join(a, "wip.txt")) {
		t.Fatal("the kept branch was not reused")
	}
}

func TestWorktreeRemoveForceAndOtherContent(t *testing.T) {
	f := newWorktreeFixture(t)
	f.create("task", "platform")
	tree := f.path("worktrees", "task")
	b := filepath.Join(tree, "platform", "b")

	// A checkout moved since, as after a schema path change, is found by
	// walking the directory.
	if err := os.MkdirAll(filepath.Join(tree, "moved"), 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, f.path("platform", "c"), "worktree", "move", filepath.Join(tree, "platform", "c"), filepath.Join(tree, "moved", "c"))
	// Other content: notes at the top, a scratch directory below.
	if err := os.MkdirAll(filepath.Join(tree, "platform", "scratch", "deep"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"NOTES.md", "platform/scratch/deep/out.log"} {
		if err := os.WriteFile(filepath.Join(tree, filepath.FromSlash(rel)), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(b, "wip.txt"), []byte("wip\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := f.refused(func(out *bytes.Buffer) error {
		return WorktreeRemove(WorktreeRemoveOptions{Name: "task"}, out)
	})
	wantLines(t, got, "removed: moved/c\n", "platform/b: uncommitted changes", "kept:\n", "  NOTES.md\n", "  platform/scratch/\n")
	if !pathExists(filepath.Join(tree, "NOTES.md")) || !pathExists(filepath.Join(b, "wip.txt")) {
		t.Fatal("content was deleted without --force")
	}

	// With the checkouts gone, other content alone still keeps the
	// directory and fails the command.
	if err := os.Remove(filepath.Join(b, "wip.txt")); err != nil {
		t.Fatal(err)
	}
	got = f.refused(func(out *bytes.Buffer) error {
		return WorktreeRemove(WorktreeRemoveOptions{Name: "task"}, out)
	})
	wantLines(t, got, "removed: platform/b\n", "kept:\n", "use --force")

	// --force deletes changes and other content alike.
	f.create("task", "platform/b")
	if err := os.WriteFile(filepath.Join(b, "wip.txt"), []byte("wip\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got = f.remove("task", true)
	wantLines(t, got, "removed: platform/b\n")
	if pathExists(f.path("worktrees")) {
		t.Fatal("--force left the worktree behind")
	}
}

func TestWorktreeScope(t *testing.T) {
	f := newWorktreeFixture(t)
	f.create("task", "services/a")
	f.create("other", "platform/b")
	tree := f.path("worktrees", "task")
	rootBranch := gitBranch(f.path("services", "a"))

	// Inside a worktree the checkout verbs act on its checkouts, with
	// paths taken from its root.
	t.Chdir(tree)
	got := f.run(func(out *bytes.Buffer) error { return Status(StatusOptions{All: true}, out) })
	wantLines(t, got, "platform/b  task\n", "services/a  task\n", "2 entries: 2 ok\n")
	if strings.Contains(got, "platform/c") || strings.Contains(got, "worktrees") {
		t.Fatalf("status in a worktree lists more than its checkouts:\n%s", got)
	}
	t.Chdir(filepath.Join(tree, "services"))
	got = f.run(func(out *bytes.Buffer) error {
		return Switch(SwitchOptions{Branches: []string{"task-2"}, Create: true}, out)
	})
	wantLines(t, got, "created: services/a\n")
	if strings.Contains(got, "platform/b") {
		t.Fatalf("switch left its subdirectory:\n%s", got)
	}
	if gitBranch(f.path("services", "a")) != rootBranch || gitBranch(f.path("worktrees", "other", "platform", "b")) != "other" {
		t.Fatal("a command in a worktree reached the root tree or a sibling")
	}
	if err := os.WriteFile(filepath.Join(tree, "services", "a", "README.md"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(filepath.Join(tree, "services", "a"))
	got = f.run(func(out *bytes.Buffer) error { return Diff(DiffOptions{Stat: true}, out) })
	wantLines(t, got, "services/a  1 file (+1 -1)\n")

	// The root tree's default branch is taken: going back to it is
	// refused by git.
	t.Chdir(tree)
	got = f.refused(func(out *bytes.Buffer) error { return Switch(SwitchOptions{Default: true}, out) })
	wantLines(t, got, "skipped: services/a\n")

	// Sync belongs to the root tree.
	got = f.refused(func(out *bytes.Buffer) error { return Sync(SyncOptions{SkipUpdate: true}, out) })
	wantLines(t, got, "sync applies the schema to the root tree")

	// rm removes from the worktree only, with the worktree's rules.
	got = f.refused(func(out *bytes.Buffer) error {
		return Remove(RemoveOptions{Paths: []string{"services/a", "platform/c"}}, out)
	})
	wantLines(t, got, "services/a: uncommitted changes (use --force)", "platform/c: nothing installed matches")
	got = f.run(func(out *bytes.Buffer) error {
		return Remove(RemoveOptions{Paths: []string{"services/a"}, Force: true}, out)
	})
	wantLines(t, got, "removed: services/a\n")
	if pathExists(filepath.Join(tree, "services")) || !isGitRepo(f.path("services", "a")) {
		t.Fatal("rm in a worktree should remove its checkout and nothing else")
	}
	state, err := loadState(f.root)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := state.Repos["a"]; !ok {
		t.Fatal("rm in a worktree dropped the root tree's state")
	}

	// worktrees/ itself belongs to no tree.
	t.Chdir(f.path("worktrees"))
	for name, command := range map[string]func(out *bytes.Buffer) error{
		"status": func(out *bytes.Buffer) error { return Status(StatusOptions{}, out) },
		"pull":   func(out *bytes.Buffer) error { return Pull(out) },
		"diff":   func(out *bytes.Buffer) error { return Diff(DiffOptions{}, out) },
		"clone":  func(out *bytes.Buffer) error { return Clone(CloneOptions{}, out) },
		"rm":     func(out *bytes.Buffer) error { return Remove(RemoveOptions{Paths: []string{"task"}}, out) },
		"sync":   func(out *bytes.Buffer) error { return Sync(SyncOptions{SkipUpdate: true}, out) },
	} {
		var out bytes.Buffer
		if err := command(&out); err == nil || !strings.Contains(err.Error(), "worktrees/") {
			t.Fatalf("%s in worktrees/: err = %v\n%s", name, err, out.String())
		}
	}
	// Removing a worktree from inside it is refused.
	t.Chdir(tree)
	got = f.refused(func(out *bytes.Buffer) error {
		return WorktreeRemove(WorktreeRemoveOptions{Name: "task"}, out)
	})
	wantLines(t, got, "leave it first")
}

// The cache is never a worktree's repository: cleaning it breaks nothing.
func TestWorktreeSurvivesCacheClean(t *testing.T) {
	f := newWorktreeFixture(t)
	f.create("task", "platform/c")
	f.run(func(out *bytes.Buffer) error { return CacheClean(CacheCleanOptions{UnusedDays: -1}, out) })
	checkout := f.path("worktrees", "task", "platform", "c")
	f.commit(checkout, "work.txt")
	t.Chdir(checkout)
	got := f.run(func(out *bytes.Buffer) error { return Push(PushOptions{SetUpstream: true}, out) })
	wantLines(t, got, "pushed: platform/c\n")
}

func TestSchemaCannotUseTheWorktreesPath(t *testing.T) {
	for _, tree := range []string{
		`"worktrees": { "x": { "$repo": { "git": "git@example.com:x.git" } } }`,
		`"worktrees/x": { "$repo": { "git": "git@example.com:x.git" } }`,
		`"worktrees": { "$repo": { "git": "git@example.com:x.git" } }`,
	} {
		def, err := decodeDefinition([]byte(`{ "version": 3, "tree": { `+tree+` } }`), "jig.json")
		if err == nil {
			_, err = flattenDefinition(def, nil)
		}
		if err == nil || !strings.Contains(err.Error(), "reserved") {
			t.Fatalf("tree %s: err = %v, want the reserved path refused", tree, err)
		}
	}
	// Below the top level the name is free.
	def, err := decodeDefinition([]byte(`{ "version": 3, "tree": { "tools/worktrees": { "$repo": { "git": "git@example.com:x.git" } } } }`), "jig.json")
	if err == nil {
		_, err = flattenDefinition(def, nil)
	}
	if err != nil {
		t.Fatalf("nested worktrees path refused: %v", err)
	}
}
