package jig

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemoveFileAndRecursiveRequirement(t *testing.T) {
	root := t.TempDir()
	writeTestWorkspace(t, root, `{
  "version": 3,
  "tree": {
    "scripts/dev.sh": {
      "$file": { "id": "dev-script", "src": "git:git@example.com:config.git#dev.sh" }
    },
    "scripts/run.sh": {
      "$file": { "id": "run-script", "src": "git:git@example.com:config.git#run.sh" }
    }
  }
}`)
	content := []byte("#!/bin/sh\n")
	for _, name := range []string{"dev.sh", "run.sh"} {
		path := filepath.Join(root, "scripts", name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	state := emptyState()
	state.Files["dev-script"] = StateFile{Path: "scripts/dev.sh", Src: "git:git@example.com:config.git#dev.sh", SHA256: sha256Hex(content)}
	state.Files["run-script"] = StateFile{Path: "scripts/run.sh", Src: "git:git@example.com:config.git#run.sh", SHA256: sha256Hex(content)}
	if err := saveState(root, state); err != nil {
		t.Fatal(err)
	}

	t.Chdir(root)

	var out bytes.Buffer
	if err := Remove(RemoveOptions{Paths: []string{"scripts"}}, &out); err == nil {
		t.Fatalf("expected -r requirement, got output:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "use -r") {
		t.Fatalf("expected -r hint, got:\n%s", out.String())
	}

	out.Reset()
	if err := Remove(RemoveOptions{Paths: []string{"scripts/dev.sh"}}, &out); err != nil {
		t.Fatalf("remove failed: %v\n%s", err, out.String())
	}
	if pathExists(filepath.Join(root, "scripts", "dev.sh")) {
		t.Fatal("expected dev.sh to be deleted")
	}
	loaded, err := loadState(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := loaded.Files["dev-script"]; ok {
		t.Fatal("expected dev-script to be dropped from state")
	}
	if _, ok := loaded.Files["run-script"]; !ok {
		t.Fatal("expected run-script to remain tracked")
	}

	// A locally modified file is refused without --force.
	if err := os.WriteFile(filepath.Join(root, "scripts", "run.sh"), []byte("edited"), 0o644); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := Remove(RemoveOptions{Paths: []string{"scripts/run.sh"}}, &out); err == nil {
		t.Fatalf("expected modified-file refusal, got:\n%s", out.String())
	}
	out.Reset()
	if err := Remove(RemoveOptions{Paths: []string{"scripts/run.sh"}, Force: true}, &out); err != nil {
		t.Fatalf("forced remove failed: %v\n%s", err, out.String())
	}
}

// A repository that git worktrees hang off is never deleted, forced or not:
// jig rm refuses it, sync --prune keeps it, and a sync that moves it
// re-links its worktrees.
func TestRepositoryWithGitWorktreesIsNotDeleted(t *testing.T) {
	root := t.TempDir()
	remote := filepath.Join(root, "remote-a")
	testRemoteRepo(t, remote)
	schema := func(tree string) string {
		return fmt.Sprintf(`{ "version": 3, "tree": { %s } }`, fmt.Sprintf(tree, remote))
	}
	writeTestWorkspace(t, root, schema(`"services/a": { "$repo": { "id": "a", "git": %q } }`))
	local := filepath.Join(root, "services", "a")
	gitIn(t, root, "clone", "-q", remote, local)
	state := emptyState()
	state.Repos["a"] = StateRepo{Path: "services/a", Git: remote}
	if err := saveState(root, state); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(root, "elsewhere", "a")
	gitIn(t, local, "worktree", "add", "-q", "-b", "task", linked)

	t.Chdir(root)

	var out bytes.Buffer
	if err := Remove(RemoveOptions{Paths: []string{"services/a"}, Force: true}, &out); err == nil {
		t.Fatalf("expected rm -f to be refused:\n%s", out.String())
	}
	if got := out.String(); !strings.Contains(got, "has git worktrees") || !strings.Contains(got, "elsewhere/a") || strings.Contains(got, "--force") {
		t.Fatalf("refusal should name the worktree and not suggest --force:\n%s", got)
	}
	if !isGitRepo(local) {
		t.Fatal("repository was deleted")
	}

	// Moved by the schema: the worktree follows its repository.
	setSchema := func(body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, sourceDir, "jig.json"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	setSchema(schema(`"apps/a": { "$repo": { "id": "a", "git": %q } }`))
	out.Reset()
	if err := Sync(SyncOptions{SkipUpdate: true}, &out); err != nil {
		t.Fatalf("sync: %v\n%s", err, out.String())
	}
	if _, err := git(linked, "status", "--porcelain"); err != nil {
		t.Fatalf("worktree lost its repository after the move: %v", err)
	}

	// Dropped from the schema: prune keeps it and says why.
	setSchema(`{ "version": 3, "tree": {} }`)
	out.Reset()
	if err := Sync(SyncOptions{SkipUpdate: true, Prune: true}, &out); err != nil {
		t.Fatalf("sync --prune: %v\n%s", err, out.String())
	}
	if got := out.String(); !strings.Contains(got, "kept:") || !strings.Contains(got, "apps/a: has git worktrees") {
		t.Fatalf("expected the repository kept for its worktrees:\n%s", got)
	}
	if !isGitRepo(filepath.Join(root, "apps", "a")) {
		t.Fatal("repository was pruned")
	}

	// A worktree whose directory is gone no longer holds the repository.
	if err := os.RemoveAll(linked); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := Sync(SyncOptions{SkipUpdate: true, Prune: true}, &out); err != nil {
		t.Fatalf("sync --prune: %v\n%s", err, out.String())
	}
	if isGitRepo(filepath.Join(root, "apps", "a")) {
		t.Fatalf("expected the repository pruned once its worktree is gone:\n%s", out.String())
	}
}
