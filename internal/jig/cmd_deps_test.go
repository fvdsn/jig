package jig

import (
	"bytes"
	"os"
	"testing"
)

func TestDepsReverseListsDirectDependents(t *testing.T) {
	root := t.TempDir()
	writeTestWorkspace(t, root, `{
  "version": 2,
  "tree": {
    "platform/auth": {
      "$repo": { "git": "git@example.com:auth.git" }
    },
    "platform/billing": {
      "$repo": {
        "git": "git@example.com:billing.git",
        "dependsOn": [{ "path": "platform/auth" }]
      }
    },
    "services/checkout": {
      "$repo": {
        "git": "git@example.com:checkout.git",
        "dependsOn": [{ "path": "platform/*" }]
      }
    },
    "services/frontend": {
      "$repo": {
        "git": "git@example.com:frontend.git",
        "dependsOn": [{ "path": "services/checkout" }]
      }
    },
    "tools/debug": {
      "$repo": {
        "git": "git@example.com:debug.git",
        "dependsOn": [{ "path": "platform/auth", "optional": true }]
      }
    },
    "legacy/old": {
      "$repo": {
        "git": "git@example.com:old.git",
        "archived": true,
        "dependsOn": [{ "path": "platform/auth" }]
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

	deps := func(options DependenciesOptions) string {
		t.Helper()
		var out bytes.Buffer
		if err := Dependencies(options, &out); err != nil {
			t.Fatalf("deps: %v\n%s", err, out.String())
		}
		return out.String()
	}

	// Direct dependents only: checkout depends on auth through the group
	// path "platform", frontend is transitive and must not appear. Optional
	// (tools/debug) and uninstalled archived (legacy/old) dependents are
	// hidden by default.
	got := deps(DependenciesOptions{Path: "platform/auth", Reverse: true})
	if want := "platform/billing\nservices/checkout\n"; got != want {
		t.Fatalf("reverse deps = %q, want %q", got, want)
	}

	// --with-optional-deps and --archived mirror the forward flags.
	got = deps(DependenciesOptions{Path: "platform/auth", Reverse: true, IncludeOptional: true})
	if want := "platform/billing\nservices/checkout\ntools/debug\n"; got != want {
		t.Fatalf("reverse deps with optional = %q, want %q", got, want)
	}
	got = deps(DependenciesOptions{Path: "platform/auth", Reverse: true, IncludeArchived: true})
	if want := "legacy/old\nplatform/billing\nservices/checkout\n"; got != want {
		t.Fatalf("reverse deps with archived = %q, want %q", got, want)
	}

	// A group target asks who depends on anything in the group. Intra-group
	// dependents count when the edge points at a different repo, but a
	// repo's own edge into its group does not list it as its own dependent.
	got = deps(DependenciesOptions{Path: "platform", Reverse: true})
	if want := "platform/billing\nservices/checkout\n"; got != want {
		t.Fatalf("reverse deps of group = %q, want %q", got, want)
	}

	got = deps(DependenciesOptions{Path: "services/checkout", Reverse: true})
	if want := "services/frontend\n"; got != want {
		t.Fatalf("reverse deps of checkout = %q, want %q", got, want)
	}
}
