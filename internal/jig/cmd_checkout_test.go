package jig

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckoutAcrossInstalledRepos(t *testing.T) {
	root := t.TempDir()
	remoteA := filepath.Join(root, "remote-a")
	remoteB := filepath.Join(root, "remote-b")
	testRemoteRepo(t, remoteA)
	testRemoteRepo(t, remoteB)
	writeTestWorkspace(t, root, fmt.Sprintf(`{
  "version": 1,
  "tree": {
    "services/a": { "$repo": { "git": %q } },
    "services/b": { "$repo": { "git": %q } }
  }
}`, remoteA, remoteB))
	localA := filepath.Join(root, "services", "a")
	localB := filepath.Join(root, "services", "b")
	gitIn(t, root, "clone", "-q", remoteA, localA)
	gitIn(t, root, "clone", "-q", remoteB, localB)
	defaultBranch := gitBranch(localA)

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

	checkout := func(options CheckoutOptions) string {
		t.Helper()
		var out bytes.Buffer
		if err := Checkout(options, &out); err != nil {
			t.Fatalf("checkout: %v\n%s", err, out.String())
		}
		return out.String()
	}

	// -b creates the branch everywhere; a second run is idempotent.
	got := checkout(CheckoutOptions{Branch: "feature", Create: true})
	if strings.Count(got, "created: ") != 2 {
		t.Fatalf("create run = %q, want 2 created", got)
	}
	got = checkout(CheckoutOptions{Branch: "feature", Create: true})
	if strings.Count(got, "present: ") != 2 {
		t.Fatalf("repeat run = %q, want 2 present", got)
	}

	// A diverging uncommitted change makes git refuse the switch: repo a is
	// skipped untouched while repo b switches back.
	if err := os.WriteFile(filepath.Join(localA, "README.md"), []byte("committed on feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, localA, "commit", "-qam", "diverge")
	if err := os.WriteFile(filepath.Join(localA, "README.md"), []byte("uncommitted edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The skip is surfaced in the exit code, while the other repo still
	// switches.
	var skipOut bytes.Buffer
	if err := Checkout(CheckoutOptions{Branch: defaultBranch}, &skipOut); err == nil {
		t.Fatalf("expected an error when a repository is skipped:\n%s", skipOut.String())
	}
	got = skipOut.String()
	if !strings.Contains(got, "switched: services/b") {
		t.Fatalf("switch run = %q, want switched: services/b", got)
	}
	if !strings.Contains(got, "skipped:") || !strings.Contains(got, "services/a") {
		t.Fatalf("switch run = %q, want services/a skipped", got)
	}
	if branch := gitBranch(localA); branch != "feature" {
		t.Fatalf("a is on %q, want feature", branch)
	}
	if data, _ := os.ReadFile(filepath.Join(localA, "README.md")); string(data) != "uncommitted edit\n" {
		t.Fatalf("local edit was not preserved: %q", data)
	}

	// A path selects a subset; -b on an existing branch switches instead of
	// failing.
	got = checkout(CheckoutOptions{Branch: "feature", Path: "services/b", Create: true})
	if !strings.Contains(got, "switched: services/b") || strings.Contains(got, "services/a") {
		t.Fatalf("subset run = %q, want only services/b switched", got)
	}

	// An invalid branch name fails before touching any repository.
	var out bytes.Buffer
	if err := Checkout(CheckoutOptions{Branch: "-bad"}, &out); err == nil {
		t.Fatal("expected invalid branch name error")
	}
}
