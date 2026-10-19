package jig

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Sync converges what is installed under the directory it runs in, from
// anywhere inside a checkout; it never materializes entries that were not
// cloned.
func TestSyncConvergesTheCurrentScope(t *testing.T) {
	root := t.TempDir()
	remoteA := testBareRemote(t, root, "remote-a")
	remoteB := testBareRemote(t, root, "remote-b")
	writeTestWorkspace(t, root, fmt.Sprintf(`{
  "version": 3,
  "tree": {
    "services/a": { "$repo": { "id": "a", "git": %q } },
    "services/b": { "$repo": { "id": "b", "git": %q } }
  }
}`, remoteA, remoteB))
	t.Chdir(root)

	var out bytes.Buffer
	if err := Clone(CloneOptions{}, &out); err != nil {
		t.Fatalf("clone: %v\n%s", err, out.String())
	}
	if err := os.RemoveAll(filepath.Join(root, "services", "b")); err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(root, "services", "a", "src")
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(inside)
	out.Reset()
	if err := Sync(SyncOptions{SkipUpdate: true}, &out); err != nil {
		t.Fatalf("sync inside a: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "present: services/a") || strings.Contains(out.String(), "services/b") {
		t.Fatalf("expected only services/a to be converged, got:\n%s", out.String())
	}
	if pathExists(filepath.Join(root, "services", "b", ".git")) {
		t.Fatal("services/b was restored although the sync ran inside services/a")
	}
	t.Chdir(root)
	out.Reset()
	if err := Sync(SyncOptions{SkipUpdate: true}, &out); err != nil {
		t.Fatalf("sync at root: %v\n%s", err, out.String())
	}
	if !pathExists(filepath.Join(root, "services", "b", ".git")) {
		t.Fatal("services/b was not restored by a root sync")
	}
}
