package jig

import (
	"bytes"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestMetaInheritedFromGroupsAndFilterSelection(t *testing.T) {
	def := testDefinition(t, `{
  "version": 1,
  "tree": {
    "platform": {
      "$group": { "meta": { "team": "core", "channel": "#platform" } },
      "auth": {
        "$repo": {
          "git": "git@gitlab.com:acme/auth.git",
          "meta": { "github-mirror": "git@github.com:acme/auth.git", "team": "identity" }
        }
      },
      "web": {
        "$repo": { "git": "git@gitlab.com:acme/web.git" }
      }
    },
    "tools/cli": {
      "$repo": { "git": "git@gitlab.com:acme/cli.git" }
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
	want := map[string]string{
		"team":          "identity", // local declaration wins over the group
		"channel":       "#platform",
		"github-mirror": "git@github.com:acme/auth.git",
	}
	if !reflect.DeepEqual(auth.Meta, want) {
		t.Fatalf("auth meta = %#v, want %#v", auth.Meta, want)
	}
	web, _ := model.entry("platform/web", EntryRepo)
	if got := web.Meta["team"]; got != "core" {
		t.Fatalf("web team = %q, want inherited %q", got, "core")
	}
	cli, _ := model.entry("tools/cli", EntryRepo)
	if cli.Meta != nil {
		t.Fatalf("cli meta = %#v, want nil", cli.Meta)
	}

	selection, err := model.Select(NodeQuery{Meta: MetaFilter{Key: "github-mirror"}})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := entryPaths(selection.Entries), []string{"platform/auth"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("key selection = %#v, want %#v", got, want)
	}

	selection, err = model.Select(NodeQuery{Meta: MetaFilter{Key: "team", Value: "core", HasValue: true}})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := entryPaths(selection.Entries), []string{"platform", "platform/web"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("key=value selection = %#v, want %#v", got, want)
	}

	selection, err = model.Select(NodeQuery{Meta: MetaFilter{Key: "team", Value: "", HasValue: true}})
	if err != nil {
		t.Fatal(err)
	}
	if len(selection.Entries) != 0 {
		t.Fatalf("empty-value selection = %#v, want none", entryPaths(selection.Entries))
	}
}

func TestMetaValidation(t *testing.T) {
	def := testDefinition(t, `{
  "version": 1,
  "tree": {
    "a": { "$repo": { "git": "git@example.com:a.git", "meta": { "ok": "fine", "not ok": "x", "k=v": "x", "$reserved": "x" } } }
  }
}`)
	result := validateDefinition(def)
	if len(result.Errors) != 3 {
		t.Fatalf("expected three validation errors, got %#v", result.Errors)
	}
}

func TestInfoShowsMeta(t *testing.T) {
	root := t.TempDir()
	writeTestWorkspace(t, root, `{
  "version": 1,
  "tree": {
    "platform": {
      "$group": { "meta": { "team": "core" } },
      "auth": {
        "$repo": {
          "git": "git@gitlab.com:acme/auth.git",
          "meta": { "github-mirror": "git@github.com:acme/auth.git" }
        }
      }
    }
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
	if err := Info(InfoOptions{Path: "platform/auth"}, &out); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); !strings.Contains(got, "meta:\n  github-mirror: git@github.com:acme/auth.git\n  team: core\n") {
		t.Fatalf("expected meta in repo info, got:\n%s", got)
	}

	out.Reset()
	if err := Info(InfoOptions{Path: "platform"}, &out); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); !strings.Contains(got, "meta:\n  team: core\n") {
		t.Fatalf("expected meta in group info, got:\n%s", got)
	}
}

func TestListFiltersByMeta(t *testing.T) {
	root := t.TempDir()
	writeTestWorkspace(t, root, `{
  "version": 1,
  "tree": {
    "platform/auth": {
      "$repo": {
        "git": "git@gitlab.com:acme/auth.git",
        "meta": { "github-mirror": "git@github.com:acme/auth.git" }
      }
    },
    "platform/web": {
      "$repo": { "git": "git@gitlab.com:acme/web.git" }
    }
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
	if err := List(ListOptions{Meta: MetaFilter{Key: "github-mirror"}, Width: -1}, &out); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "platform/auth") || strings.Contains(got, "platform/web") {
		t.Fatalf("expected only the mirrored repo, got:\n%s", got)
	}
}
