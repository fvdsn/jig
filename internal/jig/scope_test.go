package jig

import (
	"reflect"
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
