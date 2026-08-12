package jig

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestTagsListsTagVocabulary(t *testing.T) {
	root := t.TempDir()
	writeTestWorkspace(t, root, `{
  "version": 2,
  "tree": {
    "platform": {
      "$group": { "tags": ["backend"], "meta": { "team": "core" } },
      "auth": {
        "$repo": {
          "git": "git@example.com:auth.git",
          "tags": ["go"],
          "meta": { "github-mirror": "git@github.com:acme/auth.git" }
        }
      },
      "web": {
        "$repo": { "git": "git@example.com:web.git", "tags": ["js"] }
      }
    },
    "tools/cli": {
      "$repo": { "git": "git@example.com:cli.git", "tags": ["go"] }
    },
    "legacy/old": {
      "$repo": { "git": "git@example.com:old.git", "tags": ["deprecated"], "archived": true }
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

	// Counts match what filtering on each tag selects, groups included;
	// tags of uninstalled archived entries are hidden.
	var out bytes.Buffer
	if err := Tags(TagsOptions{}, &out); err != nil {
		t.Fatal(err)
	}
	want := "backend  3\n" +
		"go       2\n" +
		"js       1\n"
	if got := out.String(); got != want {
		t.Fatalf("tags output = %q, want %q", got, want)
	}

	out.Reset()
	if err := Tags(TagsOptions{IncludeArchived: true}, &out); err != nil {
		t.Fatal(err)
	}
	want = "backend     3\n" +
		"deprecated  1\n" +
		"go          2\n" +
		"js          1\n"
	if got := out.String(); got != want {
		t.Fatalf("tags --archived output = %q, want %q", got, want)
	}

	// A path argument scopes the vocabulary to the subtree.
	out.Reset()
	if err := Tags(TagsOptions{Path: "tools"}, &out); err != nil {
		t.Fatal(err)
	}
	if got, want := out.String(), "go  1\n"; got != want {
		t.Fatalf("tags tools output = %q, want %q", got, want)
	}

	// Pathless from a subdirectory scopes to that subtree, like other
	// position-relative commands.
	if err := os.MkdirAll(filepath.Join(root, "tools"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(filepath.Join(root, "tools")); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := Tags(TagsOptions{}, &out); err != nil {
		t.Fatal(err)
	}
	if got, want := out.String(), "go  1\n"; got != want {
		t.Fatalf("tags from tools/ output = %q, want %q", got, want)
	}
}
