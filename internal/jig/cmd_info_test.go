package jig

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInfoIncludesArchivedNodeWhenRequestedOrInstalled(t *testing.T) {
	root := t.TempDir()
	writeTestWorkspace(t, root, `{
  "version": 3,
  "tree": {
    "services/old": {
      "$repo": {
        "git": "git@example.com:old.git",
        "archived": true
      }
    }
  }
}`)

	t.Chdir(root)

	var out bytes.Buffer
	if err := Info(InfoOptions{Selector: Selector{Path: "services/old"}}, &out); err == nil {
		t.Fatal("expected uninstalled archived repository to be excluded")
	}
	if err := Info(InfoOptions{Selector: Selector{Path: "services/old", IncludeArchived: true}}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "path: services/old") {
		t.Fatalf("expected archived repository info, got:\n%s", out.String())
	}

	if err := exec.Command("git", "init", filepath.Join(root, "services", "old")).Run(); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := Info(InfoOptions{Selector: Selector{Path: "services/old"}}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "path: services/old") {
		t.Fatalf("expected installed archived repository info, got:\n%s", out.String())
	}
}

func TestInfoOrdersMixedGroupEntriesByPath(t *testing.T) {
	root := t.TempDir()
	writeTestWorkspace(t, root, `{
  "version": 3,
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
}`)

	t.Chdir(root)

	var out bytes.Buffer
	if err := Info(InfoOptions{Selector: Selector{Path: "services"}}, &out); err != nil {
		t.Fatal(err)
	}
	want := "path: services\n" +
		"type: group\n" +
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
