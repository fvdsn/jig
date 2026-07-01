package jig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// writeTestWorkspace lays out a workspace at root with the given schema in
// its source directory and empty state.
func writeTestWorkspace(t *testing.T, root string, schema string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, sourceDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, sourceDir, "jig.json"), []byte(schema), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := saveConfig(root, Config{Version: 1, Schema: "jig.json"}); err != nil {
		t.Fatal(err)
	}
	if err := saveState(root, emptyState()); err != nil {
		t.Fatal(err)
	}
}

func testDefinition(t *testing.T, body string) *Definition {
	t.Helper()
	var def Definition
	if err := json.Unmarshal([]byte(body), &def); err != nil {
		t.Fatal(err)
	}
	return &def
}

func testRepoEntry(path, identity string, repo Repo) Entry {
	return Entry{Path: path, Identity: identity, Kind: EntryRepo, Repo: &repo}
}

func testFileEntry(path, identity string, file File) Entry {
	return Entry{Path: path, Identity: identity, Kind: EntryFile, File: &file}
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }
