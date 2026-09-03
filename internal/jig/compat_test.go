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
	writeTestWorkspace(t, root, `{"version": 3, "tree": {}}`)

	if err := os.WriteFile(filepath.Join(root, stateFile), []byte(`{"version": 3, "repos": {}, "files": {}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadState(root); err == nil || !strings.Contains(err.Error(), "upgrade jig") {
		t.Fatalf("state guard: %v", err)
	}
	// Read-only commands load without the lock but must hit the same guard
	// rather than treating the workspace as empty.
	if _, err := loadWorkspaceAt(root, "", false); err == nil || !strings.Contains(err.Error(), "upgrade jig") {
		t.Fatalf("state guard through loadWorkspace: %v", err)
	}

	if err := os.WriteFile(filepath.Join(root, configFile), []byte(`{"version": 3, "schema": "jig.json"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig(root); err == nil || !strings.Contains(err.Error(), "upgrade jig") {
		t.Fatalf("config guard: %v", err)
	}

	// Schema guard goes through loadWorkspace.
	root2 := t.TempDir()
	writeTestWorkspace(t, root2, `{"version": 4, "tree": {}}`)
	t.Chdir(root2)
	if _, err := loadWorkspace(false); err == nil || !strings.Contains(err.Error(), "upgrade jig") {
		t.Fatalf("schema guard: %v", err)
	}

	// Version 1 predates structured references and is refused with a
	// migration hint rather than an upgrade hint.
	root3 := t.TempDir()
	writeTestWorkspace(t, root3, `{"version": 1, "tree": {}}`)
	if err := os.Chdir(root3); err != nil {
		t.Fatal(err)
	}
	if _, err := loadWorkspace(false); err == nil || !strings.Contains(err.Error(), "structured references") {
		t.Fatalf("schema v1 guard: %v", err)
	}
}

// A legacy root .jig.json is not a workspace: the error names the layout
// change instead of the generic not-found message.
func TestLegacyRootSchemaIsRefused(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".jig.json"), []byte(`{"version": 3, "tree": {}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := findWorkspace(filepath.Join(root, "sub"))
	if err == nil || !strings.Contains(err.Error(), "no longer supported") {
		t.Fatalf("legacy layout guard: %v", err)
	}
}
