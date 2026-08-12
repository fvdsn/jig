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

func TestMoveInstalledPathHandlesCaseOnlyRenames(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "Infrastructure", "dns"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Infrastructure", "dns", "main.tf"), []byte("tf"), 0o644); err != nil {
		t.Fatal(err)
	}

	// On a case-insensitive filesystem the target resolves to the source;
	// on a case-sensitive one this is a plain move. Both must succeed.
	if _, err := moveInstalledPath(root, "infrastructure/dns", "Infrastructure/dns", "infrastructure/dns", "moved"); err != nil {
		t.Fatalf("case-only move: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "infrastructure", "dns", "main.tf"))
	if err != nil || string(data) != "tf" {
		t.Fatalf("content after move: %q, %v", data, err)
	}

	// A genuinely different existing target is still a conflict.
	if err := os.MkdirAll(filepath.Join(root, "occupied"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "other"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := moveInstalledPath(root, "occupied", "other", "occupied", "moved"); err == nil {
		t.Fatal("expected conflict error for a distinct existing target")
	}
}
