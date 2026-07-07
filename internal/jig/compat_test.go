package jig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Newer version numbers in the schema, config, or state must produce a
// clear "upgrade jig" error instead of misbehaving (or, for state, silently
// stripping fields this jig does not know about on rewrite).
func TestNewerVersionsAreRefused(t *testing.T) {
	root := t.TempDir()
	writeTestWorkspace(t, root, `{"version": 1, "tree": {}}`)

	if err := os.WriteFile(filepath.Join(root, stateFile), []byte(`{"version": 2, "repos": {}, "files": {}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadState(root); err == nil || !strings.Contains(err.Error(), "upgrade jig") {
		t.Fatalf("state guard: %v", err)
	}

	if err := os.WriteFile(filepath.Join(root, configFile), []byte(`{"version": 2, "schema": "jig.json"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig(root); err == nil || !strings.Contains(err.Error(), "upgrade jig") {
		t.Fatalf("config guard: %v", err)
	}

	// Schema guard goes through loadWorkspace.
	root2 := t.TempDir()
	writeTestWorkspace(t, root2, `{"version": 2, "tree": {}}`)
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root2); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(oldWd); err != nil {
			t.Fatal(err)
		}
	}()
	if _, err := loadWorkspace(false); err == nil || !strings.Contains(err.Error(), "upgrade jig") {
		t.Fatalf("schema guard: %v", err)
	}
}
