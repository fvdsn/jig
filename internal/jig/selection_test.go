package jig

import (
	"reflect"
	"testing"
)

func TestPathMatchesSegmentBoundary(t *testing.T) {
	tests := []struct {
		path string
		item string
		want bool
	}{
		{"platform", "platform/auth", true},
		{"platform", "platform", true},
		{"platform", "platforming/api", false},
		{"platform/auth", "platform/auth", true},
		{"platform/auth", "platform/auth/extra", true},
	}
	for _, test := range tests {
		if got := pathMatches(test.path, test.item); got != test.want {
			t.Fatalf("pathMatches(%q, %q) = %v, want %v", test.path, test.item, got, test.want)
		}
	}
}

func TestNormalizeQueryPathTrimsTrailingSlashes(t *testing.T) {
	if got := normalizeQueryPath("platform/"); got != "platform" {
		t.Fatalf("normalizeQueryPath trailing slash = %q", got)
	}
	if got := normalizeQueryPath("codabox/sourcery///"); got != "codabox/sourcery" {
		t.Fatalf("normalizeQueryPath multiple trailing slashes = %q", got)
	}
}

func TestSelectNodesAppliesPathArchiveAndOrdering(t *testing.T) {
	def := testDefinition(t, `{
  "version": 2,
  "tree": {
    "services": {
      "$group": { "description": "Services" },
      "current": {
        "$repo": { "git": "git@example.com:current.git" }
      },
      "old": {
        "$repo": {
          "git": "git@example.com:old.git",
          "archived": true
        }
      },
      "scripts": {
        "current.sh": {
          "$file": { "src": "git:git@example.com:config.git#scripts/current.sh" }
        },
        "old.sh": {
          "$file": {
            "src": "git:git@example.com:config.git#scripts/old.sh",
            "archived": true
          }
        }
      }
    },
    "platform": {
      "$repo": { "git": "git@example.com:platform.git" }
    }
  }
}`)
	model, err := flattenDefinition(def)
	if err != nil {
		t.Fatal(err)
	}

	selection, err := model.Select(NodeQuery{Path: "services/"})
	if err != nil {
		t.Fatal(err)
	}
	if selection.Path != "services" {
		t.Fatalf("normalized selection path = %q", selection.Path)
	}
	if got, want := selection.repoPaths(), []string{"services/current"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("repos = %#v, want %#v", got, want)
	}
	if got, want := selection.filePaths(), []string{"services/scripts/current.sh"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("files = %#v, want %#v", got, want)
	}
	if group, ok := selection.exactGroup(); !ok || group.Path != "services" {
		t.Fatalf("entries = %#v, want services group", selection.Entries)
	}
	gotEntries := []string{}
	for _, entry := range selection.Entries {
		gotEntries = append(gotEntries, string(entry.Kind)+":"+entry.Path)
	}
	wantEntries := []string{
		"group:services",
		"repo:services/current",
		"file:services/scripts/current.sh",
	}
	if !reflect.DeepEqual(gotEntries, wantEntries) {
		t.Fatalf("entries = %#v, want %#v", gotEntries, wantEntries)
	}

	selection, err = model.Select(NodeQuery{Path: "services", IncludeArchived: true})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := selection.repoPaths(), []string{"services/current", "services/old"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("repos with archived = %#v, want %#v", got, want)
	}
	if got, want := selection.filePaths(), []string{"services/scripts/current.sh", "services/scripts/old.sh"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("files with archived = %#v, want %#v", got, want)
	}
}

func TestSelectNodesRejectsUnsafePath(t *testing.T) {
	model := Model{Entries: map[string]Entry{}}
	if _, err := model.Select(NodeQuery{Path: "../services"}); err == nil {
		t.Fatal("expected unsafe query path to fail")
	}
}

func TestSelectNodesIncludesInstalledArchivedNodes(t *testing.T) {
	def := testDefinition(t, `{
  "version": 2,
  "tree": {
    "legacy": {
      "$group": { "archived": true },
      "service": {
        "$repo": { "git": "git@example.com:legacy.git" }
      },
      "settings.json": {
        "$file": { "src": "git:git@example.com:config.git#settings.json" }
      }
    }
  }
}`)
	model, err := flattenDefinition(def)
	if err != nil {
		t.Fatal(err)
	}

	selection, err := model.Select(NodeQuery{Path: "legacy"})
	if err != nil {
		t.Fatal(err)
	}
	if len(selection.Entries) != 0 {
		t.Fatalf("unexpected uninstalled archived selection: %#v", selection)
	}

	selection, err = model.Select(NodeQuery{
		Path: "legacy",
		Installed: InstalledNodes{
			Repos: map[string]bool{"legacy/service": true},
			Files: map[string]bool{"legacy/settings.json": true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := selection.repoPaths(), []string{"legacy/service"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("installed archived repos = %#v, want %#v", got, want)
	}
	if got, want := selection.filePaths(), []string{"legacy/settings.json"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("installed archived files = %#v, want %#v", got, want)
	}
	if group, ok := selection.exactGroup(); !ok || group.Path != "legacy" {
		t.Fatalf("installed archived entries = %#v, want legacy group", selection.Entries)
	}
}

func TestTrailingSlashPathMatchesGroup(t *testing.T) {
	def := testDefinition(t, `{
  "version": 2,
  "tree": {
    "platform/auth": {
      "$repo": { "git": "git@example.com:auth.git" }
    }
  }
}`)
	model, err := flattenDefinition(def)
	if err != nil {
		t.Fatal(err)
	}
	selection, err := model.Select(NodeQuery{Path: "platform/", IncludeArchived: true})
	if err != nil {
		t.Fatal(err)
	}
	roots := selection.repoPaths()
	if !reflect.DeepEqual(roots, []string{"platform/auth"}) {
		t.Fatalf("roots = %#v", roots)
	}
}

func TestNoEntriesMatchError(t *testing.T) {
	def := testDefinition(t, `{
  "version": 2,
  "tree": {
    "sourcery/silver": {
      "$repo": { "git": "git@example.com:silver.git", "tags": ["team:sourcery", "product:codabox"] }
    },
    "flowin/app": {
      "$repo": { "git": "git@example.com:app.git", "tags": ["team:flowin"] }
    }
  }
}`)
	model, err := flattenDefinition(def)
	if err != nil {
		t.Fatal(err)
	}

	// A typo'd tag reports the near-miss instead of the empty selection.
	got := noEntriesMatchError(&model, "repositories or files", "", []string{"teams:sourcery"}).Error()
	want := `no entries carry tag "teams:sourcery"; did you mean "team:sourcery"?`
	if got != want {
		t.Fatalf("typo error = %q, want %q", got, want)
	}

	// A tag nothing resembles points at jig tags instead of guessing.
	got = noEntriesMatchError(&model, "repositories or files", "", []string{"frontend"}).Error()
	want = `no entries carry tag "frontend"; jig tags lists the tags in use`
	if got != want {
		t.Fatalf("unknown error = %q, want %q", got, want)
	}

	// Existing tags that match nothing in scope describe the query; the
	// whole-workspace scope is not rendered as an empty path.
	got = noEntriesMatchError(&model, "repositories or files", "", []string{"team:sourcery", "product:codabox"}).Error()
	want = `no repositories or files match tags team:sourcery,product:codabox`
	if got != want {
		t.Fatalf("scope error = %q, want %q", got, want)
	}
	got = noEntriesMatchError(&model, "repositories or files", "sourcery", []string{"team:flowin"}).Error()
	want = `no repositories or files match "sourcery" with tags team:flowin`
	if got != want {
		t.Fatalf("subtree error = %q, want %q", got, want)
	}
}

func TestUnknownPathSuggestion(t *testing.T) {
	def := testDefinition(t, `{
  "version": 2,
  "tree": {
    "codabox/sourcery": {
      "$group": {},
      "silver": { "$repo": { "git": "git@example.com:silver.git" } }
    },
    "flowin/core": {
      "$repo": { "git": "git@example.com:core.git", "tags": ["team:sourcery"] }
    }
  }
}`)
	model, err := flattenDefinition(def)
	if err != nil {
		t.Fatal(err)
	}

	// A typo'd path reports the near-miss entry path.
	got := noEntriesMatchError(&model, "repositories or files", "codubox/sourcery", nil).Error()
	want := `no entry is defined at "codubox/sourcery"; did you mean "codabox/sourcery"?`
	if got != want {
		t.Fatalf("typo error = %q, want %q", got, want)
	}

	// A path nothing resembles is reported as undefined, without a guess.
	got = noEntriesMatchError(&model, "repositories or files", "frontend/app", nil).Error()
	want = `no entry is defined at "frontend/app"`
	if got != want {
		t.Fatalf("unknown error = %q, want %q", got, want)
	}

	// A defined path whose selection is empty keeps the query description.
	got = noEntriesMatchError(&model, "repositories", "codabox/sourcery", []string{"team:sourcery"}).Error()
	want = `no repositories match "codabox/sourcery" with tags team:sourcery`
	if got != want {
		t.Fatalf("defined-path error = %q, want %q", got, want)
	}
}
