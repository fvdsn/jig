package jig

import (
	"bytes"
	"os"
	"reflect"
	"strings"
	"testing"
)

// validateSchema is a shorthand: parse, validate, and return the errors.
func validateSchema(t *testing.T, schema string) []string {
	t.Helper()
	return validateDefinition(testDefinition(t, schema)).Errors
}

func assertErrors(t *testing.T, errors []string, wants ...string) {
	t.Helper()
	joined := strings.Join(errors, "\n")
	for _, want := range wants {
		if !strings.Contains(joined, want) {
			t.Fatalf("errors = %#v, missing %q", errors, want)
		}
	}
}

func TestRefValidationRequiresExactlyOneSelector(t *testing.T) {
	errors := validateSchema(t, `{
  "version": 3,
  "tree": {
    "a": { "$repo": { "git": "git@example.com:a.git" } },
    "b": { "$repo": { "git": "git@example.com:b.git",
      "dependsOn": [
        { "id": "a", "path": "a" },
        {}
      ] } }
  }
}`)
	if got := strings.Count(strings.Join(errors, "\n"), "must have exactly one of id, path, and tags"); got != 2 {
		t.Fatalf("errors = %#v, want 2 exactly-one errors", errors)
	}
}

func TestRefValidationStarPlacement(t *testing.T) {
	errors := validateSchema(t, `{
  "version": 3,
  "tree": {
    "a/b": { "$repo": { "git": "git@example.com:b.git" } },
    "c": { "$repo": { "git": "git@example.com:c.git",
      "dependsOn": [
        { "path": "a/*/b" },
        { "path": "a*" }
      ] } }
  }
}`)
	if got := strings.Count(strings.Join(errors, "\n"), `"*" may only appear as the entire final segment`); got != 2 {
		t.Fatalf("errors = %#v, want 2 star placement errors", errors)
	}
}

func TestRefValidationTargets(t *testing.T) {
	errors := validateSchema(t, `{
  "version": 3,
  "tree": {
    "grp": { "$group": { "id": "the-group" } },
    "grp/a": { "$repo": { "git": "git@example.com:a.git" } },
    "undecl/x": { "$repo": { "git": "git@example.com:x.git" } },
    "docs/README.md": { "$file": { "id": "readme", "src": "git@example.com:c.git#README.md" } },
    "c": { "$repo": { "git": "git@example.com:c.git",
      "dependsOn": [
        { "path": "grp" },
        { "path": "undecl" },
        { "path": "docs" },
        { "path": "nowhere" },
        { "path": "empty/*" },
        { "id": "readme" },
        { "id": "ghost" },
        { "tags": ["nope"] }
      ] } }
  }
}`)
	assertErrors(t, errors,
		`dependency grp names a group; use "grp/*" for its members`,
		`dependency undecl does not name a declared entry; use "undecl/*" for the subtree below it`,
		// docs only contains a file, so no subtree hint is offered.
		`dependency docs does not name any entry`,
		`dependency nowhere does not name any entry`,
		`dependency empty/* does not match any repository`,
		`dependency id readme names a file, not a repository`,
		`dependency id ghost does not name any entry`,
		`dependency tags nope do not match any repository`,
	)
}

func TestRefValidationLinks(t *testing.T) {
	errors := validateSchema(t, `{
  "version": 3,
  "tree": {
    "a": { "$repo": { "git": "git@example.com:a.git", "tags": ["t"] } },
    "scripts/dev.sh": { "$file": { "id": "dev", "src": "git@example.com:c.git#dev.sh" } },
    "bad-tags": { "$file": { "link": { "tags": ["t"] } } },
    "bad-star": { "$file": { "link": { "path": "scripts/*" } } },
    "bad-kind": { "$dir": { "link": { "id": "dev" } } }
  }
}`)
	assertErrors(t, errors,
		"file bad-tags link cannot use tags",
		`file bad-star link scripts/* cannot use "/*"`,
		"dir bad-kind link id dev names a file, not a dir",
	)

	good := validateSchema(t, `{
  "version": 3,
  "tree": {
    "scripts/dev.sh": { "$file": { "id": "dev", "src": "git@example.com:c.git#dev.sh" } },
    "bin/dev": { "$file": { "link": { "id": "dev" } } }
  }
}`)
	if len(good) != 0 {
		t.Fatalf("unexpected validation errors: %#v", good)
	}
}

func TestIdentitiesAreGloballyUnique(t *testing.T) {
	errors := validateSchema(t, `{
  "version": 3,
  "tree": {
    "a": { "$repo": { "id": "shared", "git": "git@example.com:a.git" } },
    "b.md": { "$file": { "id": "shared", "src": "git@example.com:c.git#b.md" } }
  }
}`)
	assertErrors(t, errors, "duplicate identity shared: a and b.md")
}

func TestDependenciesResolveByIdTagsAndSubtree(t *testing.T) {
	def := testDefinition(t, `{
  "version": 3,
  "tree": {
    "root": { "$repo": { "git": "git@example.com:root.git",
      "dependsOn": [
        { "id": "alpha" },
        { "tags": ["base"] },
        { "path": "grp/*" }
      ] } },
    "moved/deep/a": { "$repo": { "id": "alpha", "git": "git@example.com:a.git" } },
    "b": { "$repo": { "git": "git@example.com:b.git", "tags": ["base"] } },
    "grp/c": { "$repo": { "git": "git@example.com:c.git" } },
    "grp/d/e": { "$repo": { "git": "git@example.com:e.git" } }
  }
}`)
	if errors := validateDefinition(def).Errors; len(errors) != 0 {
		t.Fatalf("unexpected validation errors: %#v", errors)
	}
	model, err := flattenDefinition(def)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := resolvePlan(&model, []string{"root"}, planOptions{IncludeRoots: false})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"b", "grp/c", "grp/d/e", "moved/deep/a"}
	if !reflect.DeepEqual(plan.Repos, want) {
		t.Fatalf("deps = %#v, want %#v", plan.Repos, want)
	}
}

func TestConditionsByIdAndExactPath(t *testing.T) {
	def := testDefinition(t, `{
  "version": 3,
  "tree": {
    "platform/auth": { "$repo": { "id": "auth", "git": "git@example.com:auth.git" } },
    "platform/billing": { "$repo": { "git": "git@example.com:billing.git" } },
    "tools/by-id": { "$repo": { "git": "git@example.com:one.git",
      "onlyWhen": { "id": "auth" } } },
    "tools/by-path": { "$repo": { "git": "git@example.com:two.git",
      "onlyWhen": { "path": "platform/billing" } } }
  }
}`)
	if errors := validateDefinition(def).Errors; len(errors) != 0 {
		t.Fatalf("unexpected validation errors: %#v", errors)
	}
	model, err := flattenDefinition(def)
	if err != nil {
		t.Fatal(err)
	}

	// auth as evidence satisfies the id condition, not the exact-path one.
	plan, err := resolvePlan(&model, []string{"platform/auth"}, planOptions{IncludeRoots: true})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"platform/auth", "tools/by-id"}; !reflect.DeepEqual(plan.Repos, want) {
		t.Fatalf("repos with auth = %#v, want %#v", plan.Repos, want)
	}

	// billing satisfies only the exact-path condition.
	plan, err = resolvePlan(&model, []string{"platform/billing"}, planOptions{IncludeRoots: true})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"platform/billing", "tools/by-path"}; !reflect.DeepEqual(plan.Repos, want) {
		t.Fatalf("repos with billing = %#v, want %#v", plan.Repos, want)
	}
}

func TestSelectByIdIgnoresPositionAndArchived(t *testing.T) {
	root := t.TempDir()
	writeTestWorkspace(t, root, `{
  "version": 3,
  "tree": {
    "services/current": { "$repo": { "id": "current", "git": "git@example.com:current.git" } },
    "services/old": { "$repo": { "id": "old", "git": "git@example.com:old.git", "archived": true } }
  }
}`)

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// Selection by id is not scoped by the working directory.
	if err := os.MkdirAll(root+"/elsewhere", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root + "/elsewhere"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(oldWd); err != nil {
			t.Fatal(err)
		}
	}()

	var out bytes.Buffer
	if err := List(ListOptions{Id: "current", Width: -1}, &out); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); !strings.Contains(got, "services/current") {
		t.Fatalf("list --id current = %q", got)
	}

	// An archived entry is selected by its id without --archived.
	out.Reset()
	if err := List(ListOptions{Id: "old", Width: -1}, &out); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); !strings.Contains(got, "services/old") {
		t.Fatalf("list --id old = %q", got)
	}

	if err := List(ListOptions{Id: "ghost", Width: -1}, &out); err == nil || !strings.Contains(err.Error(), `no entry has id "ghost"`) {
		t.Fatalf("list --id ghost error = %v", err)
	}
}

func TestQueryPathAcceptsSubtreeMarker(t *testing.T) {
	root := t.TempDir()
	writeTestWorkspace(t, root, `{
  "version": 3,
  "tree": {
    "services/api": { "$repo": { "git": "git@example.com:api.git" } },
    "platform/auth": { "$repo": { "git": "git@example.com:auth.git" } }
  }
}`)

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
	if err := List(ListOptions{Path: "services/*", Width: -1}, &out); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "services/api") || strings.Contains(got, "platform/auth") {
		t.Fatalf("list services/* = %q", got)
	}
}
