package jig

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateSafePathRejectsUnsafePaths(t *testing.T) {
	for _, path := range []string{"", ".", "..", "../outside", "foo/../bar", "~/file", "/tmp/file", "foo//bar"} {
		if err := validateSafePath(path); err == nil {
			t.Fatalf("expected %q to be invalid", path)
		}
	}
	if err := validateSafePath(".agents/skills/platform"); err != nil {
		t.Fatal(err)
	}
}

func TestPruneEmptyParentsStopsAtNonEmptyDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sourcery", "tools"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sourcery", "keep.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}

	pruneEmptyParents(root, "sourcery/tools")

	if pathExists(filepath.Join(root, "sourcery", "tools")) {
		t.Fatal("expected empty tools directory to be pruned")
	}
	if !pathExists(filepath.Join(root, "sourcery")) {
		t.Fatal("expected non-empty sourcery directory to remain")
	}
}
