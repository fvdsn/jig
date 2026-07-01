package jig

import (
	"reflect"
	"testing"
)

func TestTagsInheritedFromGroupsAndFilterSelection(t *testing.T) {
	def := testDefinition(t, `{
  "version": 1,
  "tree": {
    "platform": {
      "$group": { "tags": ["backend"] },
      "auth": {
        "$repo": { "git": "git@example.com:auth.git", "tags": ["go"] }
      },
      "web": {
        "$repo": { "git": "git@example.com:web.git", "tags": ["js"] }
      }
    },
    "tools/cli": {
      "$repo": { "git": "git@example.com:cli.git", "tags": ["go"] }
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

	auth, _ := model.entry("platform/auth", EntryRepo)
	if got, want := auth.Tags, []string{"backend", "go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("auth tags = %#v, want %#v", got, want)
	}

	selection, err := model.Select(NodeQuery{Tags: []string{"go"}})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := entryPaths(selection.Entries), []string{"platform/auth", "tools/cli"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("go selection = %#v, want %#v", got, want)
	}

	selection, err = model.Select(NodeQuery{Tags: []string{"backend", "go"}})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := entryPaths(selection.Entries), []string{"platform/auth"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("backend+go selection = %#v, want %#v", got, want)
	}

	selection, err = model.Select(NodeQuery{Tags: []string{"backend"}})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := entryPaths(selection.Entries), []string{"platform", "platform/auth", "platform/web"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("backend selection = %#v, want %#v", got, want)
	}
}

func TestTagsValidation(t *testing.T) {
	def := testDefinition(t, `{
  "version": 1,
  "tree": {
    "a": { "$repo": { "git": "git@example.com:a.git", "tags": ["ok", "not ok"] } }
  }
}`)
	result := validateDefinition(def)
	if len(result.Errors) != 1 {
		t.Fatalf("expected one validation error, got %#v", result.Errors)
	}
}
