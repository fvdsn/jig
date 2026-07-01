package jig

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInfoIncludesArchivedNodeWhenRequestedOrInstalled(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, definitionFile), []byte(`{
  "version": 1,
  "tree": {
    "services/old": {
      "$repo": {
        "git": "git@example.com:old.git",
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
	if err := Info(InfoOptions{Path: "services/old"}, &out); err == nil {
		t.Fatal("expected uninstalled archived repository to be excluded")
	}
	if err := Info(InfoOptions{Path: "services/old", IncludeArchived: true}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "path: services/old") {
		t.Fatalf("expected archived repository info, got:\n%s", out.String())
	}

	if err := exec.Command("git", "init", filepath.Join(root, "services", "old")).Run(); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := Info(InfoOptions{Path: "services/old"}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "path: services/old") {
		t.Fatalf("expected installed archived repository info, got:\n%s", out.String())
	}
}

func TestInfoOrdersMixedGroupEntriesByPath(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, definitionFile), []byte(`{
  "version": 1,
  "tree": {
    "services": {
      "$group": { "id": "services-group", "description": "Services" },
      "a-config": {
        "$file": { "src": "git:git@example.com:config.git#a-config" }
      },
      "m-api": {
        "$repo": { "git": "git@example.com:api.git" }
      },
      "z-readme": {
        "$file": { "src": "git:git@example.com:config.git#z-readme" }
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
	if err := Info(InfoOptions{Path: "services"}, &out); err != nil {
		t.Fatal(err)
	}
	want := "group: services\n" +
		"identity: services-group\n" +
		"description: Services\n" +
		"entries:\n" +
		"  file  services/a-config\n" +
		"  repo  services/m-api\n" +
		"  file  services/z-readme\n"
	if got := out.String(); got != want {
		t.Fatalf("info output:\n%s\nwant:\n%s", got, want)
	}
}
