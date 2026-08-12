package jig

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLifecycleCommandsInheritedFromGroups(t *testing.T) {
	def := testDefinition(t, `{
  "version": 2,
  "tree": {
    "services": {
      "$group": { "lint": "make check", "test": "make test" },
      "checkout": {
        "$repo": { "git": "git@example.com:checkout.git", "lint": "npm run checks" }
      },
      "billing": {
        "$repo": { "git": "git@example.com:billing.git" }
      }
    }
  }
}`)
	model, err := flattenDefinition(def)
	if err != nil {
		t.Fatal(err)
	}
	checkout, _ := model.entry("services/checkout", EntryRepo)
	if checkout.Repo.Lint != "npm run checks" {
		t.Fatalf("checkout lint = %q, want its own command", checkout.Repo.Lint)
	}
	if checkout.Repo.Test != "make test" {
		t.Fatalf("checkout test = %q, want inherited", checkout.Repo.Test)
	}
	billing, _ := model.entry("services/billing", EntryRepo)
	if billing.Repo.Lint != "make check" || billing.Repo.Test != "make test" {
		t.Fatalf("billing commands = %q/%q, want inherited", billing.Repo.Lint, billing.Repo.Test)
	}
	if billing.Repo.Setup != "" {
		t.Fatalf("billing setup = %q, want unset", billing.Repo.Setup)
	}
}

func TestLifecycleRunsCommandsAcrossRepos(t *testing.T) {
	root := t.TempDir()
	remoteA := testBareRemote(t, root, "remote-a")
	remoteB := testBareRemote(t, root, "remote-b")
	remoteC := testBareRemote(t, root, "remote-c")
	writeTestWorkspace(t, root, fmt.Sprintf(`{
  "version": 2,
  "tree": {
    "services/a": {
      "$repo": { "git": %q, "lint": "echo linted-a > lint-marker", "test": "echo boom >&2; exit 3" }
    },
    "services/b": {
      "$repo": { "git": %q, "lint": "echo linted-b > lint-marker" }
    },
    "services/c": {
      "$repo": { "git": %q }
    }
  }
}`, remoteA, remoteB, remoteC))
	for _, name := range []string{"a", "b", "c"} {
		gitIn(t, root, "clone", "-q", filepath.Join(root, "remote-"+name+".git"), filepath.Join(root, "services", name))
	}

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

	// lint runs each repo's own command in its checkout; the repo without
	// one is counted, not failed.
	var out bytes.Buffer
	if err := Lint(LifecycleOptions{}, &out); err != nil {
		t.Fatalf("lint: %v\n%s", err, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "lint: services/a") || !strings.Contains(got, "lint: services/b") {
		t.Fatalf("lint output = %q, want both repos linted", got)
	}
	if !strings.Contains(got, "1 repositories define no lint command") {
		t.Fatalf("lint output = %q, want no-command count", got)
	}
	for _, name := range []string{"a", "b"} {
		data, err := os.ReadFile(filepath.Join(root, "services", name, "lint-marker"))
		if err != nil || !strings.Contains(string(data), "linted-"+name) {
			t.Fatalf("marker for %s = %q, %v", name, data, err)
		}
	}

	// A failing command is reported with its output and fails the run;
	// other repos are unaffected.
	out.Reset()
	if err := Test(LifecycleOptions{}, &out); err == nil {
		t.Fatalf("expected test failure:\n%s", out.String())
	}
	got = out.String()
	if !strings.Contains(got, "skipped:") || !strings.Contains(got, "services/a") || !strings.Contains(got, "boom") {
		t.Fatalf("test output = %q, want services/a skipped with its output", got)
	}

	// A path scopes the run like every other command.
	out.Reset()
	if err := Lint(LifecycleOptions{Path: "services/b"}, &out); err != nil {
		t.Fatalf("lint b: %v\n%s", err, out.String())
	}
	if got := out.String(); strings.Contains(got, "services/a") || !strings.Contains(got, "lint: services/b") {
		t.Fatalf("scoped lint output = %q, want only services/b", got)
	}
}

func TestSetupRunsInDependencyOrder(t *testing.T) {
	root := t.TempDir()
	remoteApp := testBareRemote(t, root, "remote-app")
	remoteLib := testBareRemote(t, root, "remote-lib")
	// app depends on lib: lib's setup must run first even though app sorts
	// first alphabetically.
	writeTestWorkspace(t, root, fmt.Sprintf(`{
  "version": 2,
  "tree": {
    "app": {
      "$repo": { "git": %q, "setup": "echo app >> ../order.log", "dependsOn": [{ "path": "lib" }] }
    },
    "lib": {
      "$repo": { "git": %q, "setup": "echo lib >> ../order.log" }
    }
  }
}`, remoteApp, remoteLib))
	gitIn(t, root, "clone", "-q", remoteApp, filepath.Join(root, "app"))
	gitIn(t, root, "clone", "-q", remoteLib, filepath.Join(root, "lib"))

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

	var out bytes.Buffer
	if err := Setup(LifecycleOptions{}, &out); err != nil {
		t.Fatalf("setup: %v\n%s", err, out.String())
	}
	data, err := os.ReadFile(filepath.Join(root, "order.log"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), "lib\napp\n"; got != want {
		t.Fatalf("order = %q, want %q", got, want)
	}
	// The report reflects the same order.
	output := out.String()
	if strings.Index(output, "setup: lib") > strings.Index(output, "setup: app") {
		t.Fatalf("report order = %q, want lib before app", output)
	}
}
