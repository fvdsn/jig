package jig

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func testRemoteRepo(t *testing.T, dir string) {
	t.Helper()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	run("init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-qm", "init")
}

func TestCloneRepoUsesCacheAndStaysIndependent(t *testing.T) {
	root := t.TempDir()
	cache := filepath.Join(root, "cache")
	t.Setenv("JIG_CACHE_DIR", cache)
	remote := filepath.Join(root, "remote")
	testRemoteRepo(t, remote)

	first := filepath.Join(root, "first")
	if err := cloneRepo(remote, first); err != nil {
		t.Fatal(err)
	}
	mirror := mirrorDir(cache, remote)
	if !pathExists(filepath.Join(mirror, "HEAD")) {
		t.Fatalf("expected mirror at %s", mirror)
	}
	if origin, err := gitOrigin(first); err != nil || origin != remote {
		t.Fatalf("origin = %q, %v; want %q", origin, err, remote)
	}

	// Second clone comes from the freshened mirror.
	second := filepath.Join(root, "second")
	if err := cloneRepo(remote, second); err != nil {
		t.Fatal(err)
	}
	if !pathExists(filepath.Join(second, "README.md")) {
		t.Fatal("expected checkout content")
	}

	// Deleting the cache must not affect existing clones.
	if err := os.RemoveAll(cache); err != nil {
		t.Fatal(err)
	}
	if _, err := git(first, "fsck", "--no-progress"); err != nil {
		t.Fatalf("clone broken after cache removal: %v", err)
	}

	// And the cache repopulates transparently on the next clone.
	third := filepath.Join(root, "third")
	if err := cloneRepo(remote, third); err != nil {
		t.Fatal(err)
	}
	if !pathExists(filepath.Join(mirror, "HEAD")) {
		t.Fatal("expected mirror to be recreated")
	}
}

func TestCloneRepoFallsBackWhenCacheDisabled(t *testing.T) {
	root := t.TempDir()
	t.Setenv("JIG_CACHE_DIR", "")
	remote := filepath.Join(root, "remote")
	testRemoteRepo(t, remote)

	target := filepath.Join(root, "clone")
	if err := cloneRepo(remote, target); err != nil {
		t.Fatal(err)
	}
	if !pathExists(filepath.Join(target, "README.md")) {
		t.Fatal("expected checkout content")
	}
}

func TestCacheShowFile(t *testing.T) {
	root := t.TempDir()
	t.Setenv("JIG_CACHE_DIR", filepath.Join(root, "cache"))
	remote := filepath.Join(root, "remote")
	testRemoteRepo(t, remote)

	data, err := cacheShowFile(remote, "", "README.md")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello\n" {
		t.Fatalf("content = %q", data)
	}
}
