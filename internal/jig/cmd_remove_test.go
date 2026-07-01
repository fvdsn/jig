package jig

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemoveFileAndRecursiveRequirement(t *testing.T) {
	root := t.TempDir()
	writeTestWorkspace(t, root, `{
  "version": 1,
  "tree": {
    "scripts/dev.sh": {
      "$file": { "id": "dev-script", "src": "git:git@example.com:config.git#dev.sh" }
    },
    "scripts/run.sh": {
      "$file": { "id": "run-script", "src": "git:git@example.com:config.git#run.sh" }
    }
  }
}`)
	content := []byte("#!/bin/sh\n")
	for _, name := range []string{"dev.sh", "run.sh"} {
		path := filepath.Join(root, "scripts", name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	state := emptyState()
	state.Files["dev-script"] = StateFile{Path: "scripts/dev.sh", Src: "git:git@example.com:config.git#dev.sh", SHA256: sha256Hex(content)}
	state.Files["run-script"] = StateFile{Path: "scripts/run.sh", Src: "git:git@example.com:config.git#run.sh", SHA256: sha256Hex(content)}
	if err := saveState(root, state); err != nil {
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
	if err := Remove(RemoveOptions{Paths: []string{"scripts"}}, &out); err == nil {
		t.Fatalf("expected -r requirement, got output:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "use -r") {
		t.Fatalf("expected -r hint, got:\n%s", out.String())
	}

	out.Reset()
	if err := Remove(RemoveOptions{Paths: []string{"scripts/dev.sh"}}, &out); err != nil {
		t.Fatalf("remove failed: %v\n%s", err, out.String())
	}
	if pathExists(filepath.Join(root, "scripts", "dev.sh")) {
		t.Fatal("expected dev.sh to be deleted")
	}
	loaded, err := loadState(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := loaded.Files["dev-script"]; ok {
		t.Fatal("expected dev-script to be dropped from state")
	}
	if _, ok := loaded.Files["run-script"]; !ok {
		t.Fatal("expected run-script to remain tracked")
	}

	// A locally modified file is refused without --force.
	if err := os.WriteFile(filepath.Join(root, "scripts", "run.sh"), []byte("edited"), 0o644); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := Remove(RemoveOptions{Paths: []string{"scripts/run.sh"}}, &out); err == nil {
		t.Fatalf("expected modified-file refusal, got:\n%s", out.String())
	}
	out.Reset()
	if err := Remove(RemoveOptions{Paths: []string{"scripts/run.sh"}, Force: true}, &out); err != nil {
		t.Fatalf("forced remove failed: %v\n%s", err, out.String())
	}
}
