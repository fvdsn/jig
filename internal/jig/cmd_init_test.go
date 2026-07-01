package jig

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestInitFromLocalFileCreatesSourceCheckout(t *testing.T) {
	root := t.TempDir()
	sourcePath := filepath.Join(root, "source.jig.json")
	workspacePath := filepath.Join(root, "workspace")
	if err := os.WriteFile(sourcePath, []byte(`{
  "version": 1,
  "tree": {
    "platform/auth": {
      "$repo": { "id": "auth-service", "git": "git@example.com:auth.git" }
    }
  }
}`), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := Init(InitOptions{
		SourceArg:    sourcePath,
		WorkspaceDir: workspacePath,
	}, &out); err != nil {
		t.Fatal(err)
	}

	config, err := loadConfig(workspacePath)
	if err != nil {
		t.Fatal(err)
	}
	if config.Schema != "jig.json" {
		t.Fatalf("expected schema jig.json, got %q", config.Schema)
	}
	def, err := loadDefinition(filepath.Join(workspacePath, sourceDir, "jig.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := flattenDefinition(def); err != nil {
		t.Fatal(err)
	}
	if !isGitRepo(filepath.Join(workspacePath, sourceDir)) {
		t.Fatal("expected source checkout to be a git repository")
	}
	state, err := loadState(workspacePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Repos) != 0 || len(state.Files) != 0 {
		t.Fatalf("expected empty state, got %#v", state)
	}
}

func TestInitFromLocalFileRejectsPathFlag(t *testing.T) {
	root := t.TempDir()
	sourcePath := filepath.Join(root, "source.jig.json")
	if err := os.WriteFile(sourcePath, []byte(`{"version":1,"tree":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	err := Init(InitOptions{
		SourceArg:    sourcePath,
		WorkspaceDir: filepath.Join(root, "workspace"),
		SchemaPath:   "nested/jig.json",
	}, ioDiscard{})
	if err == nil || err.Error() != "--path can only be used with Git sources" {
		t.Fatalf("expected --path error, got %v", err)
	}
}
