package jig

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSwitchAcrossInstalledRepos(t *testing.T) {
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
	defaultBranch := gitBranch(localA)

	t.Chdir(root)

	switchTo := func(options SwitchOptions) string {
		t.Helper()
		var out bytes.Buffer
		if err := Switch(options, &out); err != nil {
			t.Fatalf("switch: %v\n%s", err, out.String())
		}
		return out.String()
	}

	// -b creates the branch everywhere; a second run is idempotent.
	got := switchTo(SwitchOptions{Branches: []string{"feature"}, Create: true})
	if strings.Count(got, "created: ") != 2 {
		t.Fatalf("create run = %q, want 2 created", got)
	}
	got = switchTo(SwitchOptions{Branches: []string{"feature"}, Create: true})
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
	if err := Switch(SwitchOptions{Branches: []string{defaultBranch}}, &skipOut); err == nil {
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

	// The directory the command runs in selects a subset, from anywhere
	// inside the checkout; -b on an existing branch switches instead of
	// failing.
	inside := filepath.Join(localB, "src")
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(inside)
	got = switchTo(SwitchOptions{Branches: []string{"feature"}, Create: true})
	if !strings.Contains(got, "switched: services/b") || strings.Contains(got, "services/a") {
		t.Fatalf("subset run = %q, want only services/b switched", got)
	}
	t.Chdir(root)

	// An invalid branch name fails before touching any repository.
	var out bytes.Buffer
	if err := Switch(SwitchOptions{Branches: []string{"-bad"}}, &out); err == nil {
		t.Fatal("expected invalid branch name error")
	}
}

func TestSwitchDefaultBranch(t *testing.T) {
	root := t.TempDir()
	remoteA := filepath.Join(root, "remote-a")
	remoteB := filepath.Join(root, "remote-b")
	testRemoteRepo(t, remoteA)
	testRemoteRepo(t, remoteB)
	// Different default branches per remote is the scenario --default
	// exists for.
	gitIn(t, remoteB, "branch", "-m", "trunk")
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
	defaultA := gitBranch(localA)

	t.Chdir(root)

	switchTo := func(options SwitchOptions) string {
		t.Helper()
		var out bytes.Buffer
		if err := Switch(options, &out); err != nil {
			t.Fatalf("switch: %v\n%s", err, out.String())
		}
		return out.String()
	}

	switchTo(SwitchOptions{Branches: []string{"feature"}, Create: true})
	// Dropping origin/HEAD in b exercises the ls-remote fallback.
	gitIn(t, localB, "symbolic-ref", "--delete", "refs/remotes/origin/HEAD")

	got := switchTo(SwitchOptions{Default: true})
	if !strings.Contains(got, fmt.Sprintf("switched: services/a (%s)", defaultA)) ||
		!strings.Contains(got, "switched: services/b (trunk)") {
		t.Fatalf("default run = %q, want both repos switched to their own default", got)
	}
	if branch := gitBranch(localB); branch != "trunk" {
		t.Fatalf("b is on %q, want trunk", branch)
	}
	// The fallback cached the answer, so the next resolution is offline.
	if _, err := git(localB, "symbolic-ref", "refs/remotes/origin/HEAD"); err != nil {
		t.Fatalf("expected origin/HEAD to be recorded after the fallback: %v", err)
	}

	got = switchTo(SwitchOptions{Default: true})
	if strings.Count(got, "present: ") != 2 {
		t.Fatalf("repeat run = %q, want 2 present", got)
	}
}

// A branch list takes the first branch each repository has, locally or on
// origin, naming the choice; --default is the last fallback, and without
// it a repository with none of the branches is skipped.
func TestSwitchBranchList(t *testing.T) {
	root := t.TempDir()
	remotes := map[string]string{}
	for _, name := range []string{"a", "b", "c"} {
		remotes[name] = filepath.Join(root, "remote-"+name)
		testRemoteRepo(t, remotes[name])
	}
	gitIn(t, remotes["a"], "branch", "release/CX-1")
	gitIn(t, remotes["b"], "branch", "feature/CX-1")
	writeTestWorkspace(t, root, fmt.Sprintf(`{
  "version": 3,
  "tree": {
    "services/a": { "$repo": { "git": %q } },
    "services/b": { "$repo": { "git": %q } },
    "services/c": { "$repo": { "git": %q } }
  }
}`, remotes["a"], remotes["b"], remotes["c"]))
	for _, name := range []string{"a", "b", "c"} {
		gitIn(t, root, "clone", "-q", remotes[name], filepath.Join(root, "services", name))
	}
	defaultC := gitBranch(filepath.Join(root, "services", "c"))
	t.Chdir(root)

	var out bytes.Buffer
	err := Switch(SwitchOptions{Branches: []string{"release/CX-1", "feature/CX-1"}}, &out)
	got := out.String()
	if err == nil || !strings.Contains(got, "switched: services/a (release/CX-1)") || !strings.Contains(got, "switched: services/b (feature/CX-1)") || !strings.Contains(got, "services/c: none of release/CX-1, feature/CX-1 exists") {
		t.Fatalf("list run = %v\n%s", err, got)
	}
	out.Reset()
	if err := Switch(SwitchOptions{Branches: []string{"release/CX-1", "feature/CX-1"}, Default: true}, &out); err != nil {
		t.Fatalf("list with default: %v\n%s", err, out.String())
	}
	got = out.String()
	if !strings.Contains(got, "present: services/a (release/CX-1)") || !strings.Contains(got, fmt.Sprintf("present: services/c (%s)", defaultC)) {
		t.Fatalf("default fallback run =\n%s", got)
	}
	// -c on a branch that exists on origin only switches to a tracking
	// branch instead of creating a second one.
	out.Reset()
	if err := Switch(SwitchOptions{Branches: []string{"release/CX-1"}, Create: true}, &out); err != nil {
		t.Fatalf("-c existing: %v\n%s", err, out.String())
	}
	if got := out.String(); !strings.Contains(got, "present: services/a") || strings.Count(got, "created: ") != 2 {
		t.Fatalf("-c run =\n%s", got)
	}
	if upstream, err := git(filepath.Join(root, "services", "a"), "rev-parse", "--abbrev-ref", "release/CX-1@{upstream}"); err != nil || strings.TrimSpace(upstream) != "origin/release/CX-1" {
		t.Fatalf("upstream = %q %v", upstream, err)
	}
	if err := Switch(SwitchOptions{Branches: []string{"x", "y"}, Create: true}, &out); err == nil {
		t.Fatal("-c with two names accepted")
	}
	if err := Switch(SwitchOptions{}, &out); err == nil {
		t.Fatal("no branch accepted")
	}
}
