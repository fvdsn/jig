package jig

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// A pathless sync converges what is installed, but --id names an entry
// explicitly and must materialize it like a path would.
func TestSyncByIdMaterializesTheEntry(t *testing.T) {
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
	if err := Sync(SyncOptions{Selector: Selector{ID: "b"}, SkipUpdate: true}, &out); err != nil {
		t.Fatalf("sync --id b: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "cloned: services/b") {
		t.Fatalf("expected services/b to be cloned, got:\n%s", out.String())
	}
	if !pathExists(filepath.Join(root, "services", "b", ".git")) {
		t.Fatal("services/b was not cloned")
	}
	if pathExists(filepath.Join(root, "services", "a")) {
		t.Fatal("services/a was cloned although only b was selected")
	}
}
