package jig

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitRunsVerbatimInScope(t *testing.T) {
	root := t.TempDir()
	remoteA := filepath.Join(root, "remote-a")
	remoteB := filepath.Join(root, "remote-b")
	testRemoteRepo(t, remoteA)
	testRemoteRepo(t, remoteB)
	writeTestWorkspace(t, root, fmt.Sprintf(`{
  "version": 3,
  "tree": {
    "services/a": { "$repo": { "git": %q } },
    "services/b": { "$repo": { "git": %q } }
  }
}`, remoteA, remoteB))
	localA := filepath.Join(root, "services", "a")
	localB := filepath.Join(root, "services", "b")
	gitIn(t, root, "clone", "-q", remoteA, localA)
	gitIn(t, root, "clone", "-q", remoteB, localB)
	t.Chdir(root)

	run := func(args ...string) (string, error) {
		t.Helper()
		var out bytes.Buffer
		err := Git(args, &out)
		return out.String(), err
	}

	// Every repository in scope reports in path order, "<path>: <output>".
	got, err := run("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		t.Fatalf("git rev-parse: %v\n%s", err, got)
	}
	branch := gitBranch(localA)
	want := fmt.Sprintf("services/a: %s\nservices/b: %s\n", branch, branch)
	if got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}

	// Multi-line output continues indented; a silent command reports done.
	got, err = run("log", "--format=%s%n%s", "-1")
	if err != nil {
		t.Fatalf("git log: %v\n%s", err, got)
	}
	if !strings.Contains(got, "services/a: init\n  init\n") {
		t.Fatalf("multi-line output = %q, want indented continuation", got)
	}
	got, err = run("update-index", "--refresh")
	if err != nil {
		t.Fatalf("git update-index: %v\n%s", err, got)
	}
	if got != "services/a: done\nservices/b: done\n" {
		t.Fatalf("silent output = %q, want done lines", got)
	}

	// A failing command lists the repository in the failed group, with
	// git's message in its line, and fails the command.
	got, err = run("rev-parse", "--verify", "no-such-ref")
	if err == nil {
		t.Fatalf("expected a failure:\n%s", got)
	}
	if !strings.Contains(got, "services/a: fatal:") || !strings.Contains(got, "failed:\n  services/a: exit status 128\n  services/b: exit status 128\n") {
		t.Fatalf("failed output = %q", got)
	}

	// The directory the command runs in scopes it, from anywhere inside a
	// checkout.
	inside := filepath.Join(localB, "src")
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(inside)
	got, err = run("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		t.Fatalf("scoped git: %v\n%s", err, got)
	}
	if got != "services/b: "+branch+"\n" {
		t.Fatalf("scoped output = %q, want only services/b", got)
	}

	// No command is an error before any repository runs.
	if _, err := run(); err == nil {
		t.Fatal("expected an error without a git command")
	}
}
