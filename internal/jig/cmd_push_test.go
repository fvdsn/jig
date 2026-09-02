package jig

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testBareRemote creates a bare repository seeded with one commit, so
// workspace clones can push back to it.
func testBareRemote(t *testing.T, root string, name string) string {
	t.Helper()
	seed := filepath.Join(root, name+"-seed")
	testRemoteRepo(t, seed)
	bare := filepath.Join(root, name+".git")
	gitIn(t, root, "clone", "-q", "--bare", seed, bare)
	return bare
}

func TestPushAcrossInstalledRepos(t *testing.T) {
	root := t.TempDir()
	remoteA := testBareRemote(t, root, "remote-a")
	remoteB := testBareRemote(t, root, "remote-b")
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

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(oldWd); err != nil {
			t.Fatal(err)
		}
	}()

	push := func(options PushOptions) string {
		t.Helper()
		var out bytes.Buffer
		if err := Push(options, &out); err != nil {
			t.Fatalf("push: %v\n%s", err, out.String())
		}
		return out.String()
	}
	commitIn := func(local string, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(local, "README.md"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		gitIn(t, local, "commit", "-qam", "change")
	}

	// Fresh clones have nothing to push.
	got := push(PushOptions{})
	if strings.Count(got, "up to date: ") != 2 {
		t.Fatalf("initial run = %q, want 2 up to date", got)
	}

	// Only the repository with new commits pushes; the remote receives them.
	commitIn(localA, "one\n")
	got = push(PushOptions{})
	if !strings.Contains(got, "pushed: services/a") || !strings.Contains(got, "up to date: services/b") {
		t.Fatalf("push run = %q, want a pushed and b up to date", got)
	}
	localHead, _ := git(localA, "rev-parse", "HEAD")
	remoteHead, _ := git(remoteA, "rev-parse", "HEAD")
	if localHead != remoteHead {
		t.Fatalf("remote head = %q, want %q", remoteHead, localHead)
	}

	// A fresh branch has no upstream: skipped without -u, pushed with it,
	// and a re-run is idempotent.
	var out bytes.Buffer
	if err := Checkout(CheckoutOptions{Branch: "feature", Create: true}, &out); err != nil {
		t.Fatalf("checkout: %v\n%s", err, out.String())
	}
	commitIn(localA, "two\n")
	out.Reset()
	if err := Push(PushOptions{}, &out); err == nil {
		t.Fatalf("expected no-upstream repositories to be skipped:\n%s", out.String())
	}
	if got := out.String(); !strings.Contains(got, "skipped:") || !strings.Contains(got, "use -u") {
		t.Fatalf("no-upstream run = %q, want skipped with -u hint", got)
	}
	got = push(PushOptions{SetUpstream: true})
	if strings.Count(got, "pushed: ") != 2 {
		t.Fatalf("-u run = %q, want 2 pushed", got)
	}
	if _, err := git(remoteA, "rev-parse", "--verify", "refs/heads/feature"); err != nil {
		t.Fatalf("remote-a has no feature branch: %v", err)
	}
	got = push(PushOptions{})
	if strings.Count(got, "up to date: ") != 2 {
		t.Fatalf("repeat run = %q, want 2 up to date", got)
	}

	// A diverged repository is never force-pushed: the rejection is
	// reported and the remote keeps its commits.
	gitIn(t, localA, "reset", "-q", "--hard", "HEAD~1")
	commitIn(localA, "diverged\n")
	remoteHead, _ = git(remoteA, "rev-parse", "refs/heads/feature")
	out.Reset()
	if err := Push(PushOptions{}, &out); err == nil {
		t.Fatalf("expected the diverged repository to be skipped:\n%s", out.String())
	}
	if got := out.String(); !strings.Contains(got, "skipped:") || !strings.Contains(got, "services/a") {
		t.Fatalf("diverged run = %q, want services/a skipped", got)
	}
	if head, _ := git(remoteA, "rev-parse", "refs/heads/feature"); head != remoteHead {
		t.Fatalf("remote feature moved to %q, want %q kept", head, remoteHead)
	}

	// A detached HEAD has no branch to push.
	gitIn(t, localB, "checkout", "-q", "--detach")
	out.Reset()
	if err := Push(PushOptions{Path: "services/b"}, &out); err == nil {
		t.Fatalf("expected the detached repository to be skipped:\n%s", out.String())
	}
	if got := out.String(); !strings.Contains(got, "detached HEAD") {
		t.Fatalf("detached run = %q, want detached HEAD skip", got)
	}
}
