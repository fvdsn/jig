package jig

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestListSupportsPathAndArchivedFlag(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, definitionFile), []byte(`{
  "version": 1,
  "tree": {
    "services/current": {
      "$repo": { "git": "git@example.com:current.git" }
    },
    "services/old": {
      "$repo": {
        "git": "git@example.com:old.git",
        "archived": true
      }
    },
    "platform/auth": {
      "$repo": { "git": "git@example.com:auth.git" }
    },
    "services/scripts/current.sh": {
      "$file": { "src": "git:git@example.com:config.git#scripts/current.sh" }
    },
    "services/scripts/old.sh": {
      "$file": {
        "src": "git:git@example.com:config.git#scripts/old.sh",
        "archived": true
      }
    }
  }
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := saveState(root, emptyState()); err != nil {
		t.Fatal(err)
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

	var out bytes.Buffer
	if err := List(ListOptions{Path: "services/"}, &out); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "repo  services/current") || !strings.Contains(got, "file  services/scripts/current.sh") {
		t.Fatalf("expected current services entries, got:\n%s", got)
	}
	if strings.Contains(got, "platform/auth") || strings.Contains(got, "services/old") || strings.Contains(got, "services/scripts/old.sh") {
		t.Fatalf("unexpected filtered list output:\n%s", got)
	}

	out.Reset()
	if err := List(ListOptions{Path: "services/", IncludeArchived: true}, &out); err != nil {
		t.Fatal(err)
	}
	got = out.String()
	if !strings.Contains(got, "repo  services/old") || !strings.Contains(got, "file  services/scripts/old.sh") {
		t.Fatalf("expected archived services entries, got:\n%s", got)
	}
}
