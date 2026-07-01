package jig

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestInitFromLocalFileDoesNotSetSource(t *testing.T) {
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
		SourceArg:      sourcePath,
		WorkspaceDir:   workspacePath,
		DefinitionPath: DefaultDefinitionPath,
	}, &out); err != nil {
		t.Fatal(err)
	}

	def, err := loadDefinition(filepath.Join(workspacePath, definitionFile))
	if err != nil {
		t.Fatal(err)
	}
	if def.Source != nil {
		t.Fatalf("expected local init not to set source, got %#v", def.Source)
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
		SourceArg:      sourcePath,
		WorkspaceDir:   filepath.Join(root, "workspace"),
		DefinitionPath: "nested/.jig.json",
	}, ioDiscard{})
	if err == nil || err.Error() != "--path can only be used with Git sources" {
		t.Fatalf("expected --path error, got %v", err)
	}
}
