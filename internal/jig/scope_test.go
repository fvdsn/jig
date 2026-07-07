package jig

import (
	"reflect"
	"strings"
	"testing"
)

func TestScopeActivationFollowsNearbyRepos(t *testing.T) {
	def := testDefinition(t, `{
  "version": 1,
  "tree": {
    "platform/auth": {
      "$repo": { "git": "git@example.com:auth.git" }
    },
    "platform/scripts/dev.sh": {
      "$file": { "src": "git:git@example.com:config.git#dev.sh" }
    },
    "billing/api": {
      "$repo": { "git": "git@example.com:billing.git" }
    },
    "billing/tools": {
      "$dir": { "src": "git:git@example.com:config.git#tools" }
    },
    "SKILL.md": {
      "$file": { "src": "git:git@example.com:config.git#SKILL.md" }
    }
  }
}`)
	model, err := flattenDefinition(def)
	if err != nil {
		t.Fatal(err)
	}

	// Cloning platform/auth activates the platform support file and the
	// root-scoped file, but not billing's support dir.
	resolved, err := resolvePlan(&model, []string{"platform/auth"}, planOptions{IncludeRoots: true})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"SKILL.md", "platform/scripts/dev.sh"}; !reflect.DeepEqual(resolved.Files, want) {
		t.Fatalf("files = %#v, want %#v", resolved.Files, want)
	}
	if len(resolved.Dirs) != 0 {
		t.Fatalf("dirs = %#v, want none", resolved.Dirs)
	}

	// Cloning billing/api activates billing/tools instead.
	resolved, err = resolvePlan(&model, []string{"billing/api"}, planOptions{IncludeRoots: true})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"billing/tools"}; !reflect.DeepEqual(resolved.Dirs, want) {
		t.Fatalf("dirs = %#v, want %#v", resolved.Dirs, want)
	}
	if want := []string{"SKILL.md"}; !reflect.DeepEqual(resolved.Files, want) {
		t.Fatalf("files = %#v, want %#v", resolved.Files, want)
	}

	// An installed repo (not just an active one) activates its scope too.
	resolved, err = resolvePlan(&model, nil, planOptions{
		Installed: map[string]bool{"platform/auth": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"SKILL.md", "platform/scripts/dev.sh"}; !reflect.DeepEqual(resolved.Files, want) {
		t.Fatalf("files with installed repo = %#v, want %#v", resolved.Files, want)
	}

	// Nothing active or installed: no files at all.
	resolved, err = resolvePlan(&model, nil, planOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved.Files)+len(resolved.Dirs) != 0 {
		t.Fatalf("expected no active files/dirs, got %#v %#v", resolved.Files, resolved.Dirs)
	}
}

func TestTagConditionsActivateByEvidenceTags(t *testing.T) {
	def := testDefinition(t, `{
  "version": 1,
  "tree": {
    "services/api": {
      "$repo": { "git": "git@example.com:api.git", "tags": ["api", "go"] }
    },
    "services/web": {
      "$repo": { "git": "git@example.com:web.git", "tags": ["frontend"] }
    },
    "tools/api-console": {
      "$repo": { "git": "git@example.com:console.git",
        "onlyWhen": { "tags": ["api"] } }
    },
    "docs/API.md": {
      "$file": { "src": "git@example.com:config.git#API.md",
        "onlyWhen": { "path": "services", "tags": ["api", "go"] } }
    }
  }
}`)
	if result := validateDefinition(def); len(result.Errors) > 0 {
		t.Fatalf("unexpected validation errors: %#v", result.Errors)
	}
	model, err := flattenDefinition(def)
	if err != nil {
		t.Fatal(err)
	}

	// A frontend repo satisfies neither the tag condition nor the combined one.
	resolved, err := resolvePlan(&model, []string{"services/web"}, planOptions{IncludeRoots: true})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := resolved.Repos, []string{"services/web"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("repos = %#v, want %#v", got, want)
	}
	if len(resolved.Files) != 0 {
		t.Fatalf("files = %#v, want none", resolved.Files)
	}

	// The api-tagged repo activates the tag-gated repo and the path+tags file.
	resolved, err = resolvePlan(&model, []string{"services/api"}, planOptions{IncludeRoots: true})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := resolved.Repos, []string{"services/api", "tools/api-console"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("repos = %#v, want %#v", got, want)
	}
	if got, want := resolved.Files, []string{"docs/API.md"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("files = %#v, want %#v", got, want)
	}

	// An installed api repo counts as evidence too.
	resolved, err = resolvePlan(&model, nil, planOptions{
		Installed: map[string]bool{"services/api": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := resolved.Repos, []string{"tools/api-console"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("repos with installed evidence = %#v, want %#v", got, want)
	}
}

func TestTagConditionValidation(t *testing.T) {
	def := testDefinition(t, `{
  "version": 1,
  "tree": {
    "services/api": {
      "$repo": { "git": "git@example.com:api.git", "tags": ["api"] }
    },
    "a": { "$repo": { "git": "git@example.com:a.git",
      "onlyWhen": { "tags": ["nonexistent"] } } },
    "b": { "$repo": { "git": "git@example.com:b.git",
      "onlyWhen": { "reason": "empty condition" } } },
    "c": { "$repo": { "git": "git@example.com:c.git",
      "onlyWhen": { "path": "services", "tags": ["frontend"] } } }
  }
}`)
	result := validateDefinition(def)
	joined := strings.Join(result.Errors, "\n")
	for _, want := range []string{
		"a onlyWhen tags nonexistent does not match any repository",
		"b has onlyWhen without a path or tags",
		"c onlyWhen services tags frontend does not match any repository",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("errors = %#v, missing %q", result.Errors, want)
		}
	}
}
