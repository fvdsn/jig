package jig

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestStatusSkipsArchivedMissingEntriesUnlessIncluded(t *testing.T) {
	root := t.TempDir()
	writeTestWorkspace(t, root, `{
  "version": 1,
  "tree": {
    "services/current": {
      "$repo": { "git": "git@example.com:current.git" }
    },
    "services/old": {
      "$repo": {
        "git": "git@example.com:old.git",
        "archived": true
      }
    },
    "scripts/current.sh": {
      "$file": { "src": "git:git@example.com:config.git#scripts/current.sh" }
    },
    "scripts/old.sh": {
      "$file": {
        "src": "git:git@example.com:config.git#scripts/old.sh",
        "archived": true
      }
    }
  }
}`)

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
	if err := Status(StatusOptions{}, &out); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "services/current") {
		t.Fatalf("expected current repo in status, got:\n%s", got)
	}
	// With no repository installed, scope-activated files are inactive.
	if strings.Contains(got, "scripts/current.sh") {
		t.Fatalf("did not expect inactive file in status, got:\n%s", got)
	}
	if strings.Contains(got, "services/old") || strings.Contains(got, "scripts/old.sh") {
		t.Fatalf("did not expect archived entries in status, got:\n%s", got)
	}

	oldRepoPath := filepath.Join(root, "services", "old")
	if err := exec.Command("git", "init", oldRepoPath).Run(); err != nil {
		t.Fatal(err)
	}
	oldFilePath := filepath.Join(root, "scripts", "old.sh")
	if err := os.MkdirAll(filepath.Dir(oldFilePath), 0o755); err != nil {
		t.Fatal(err)
	}
	oldFileContents := []byte("old")
	if err := os.WriteFile(oldFilePath, oldFileContents, 0o644); err != nil {
		t.Fatal(err)
	}
	state := emptyState()
	state.Files["scripts/old.sh"] = StateFile{
		Path:   "scripts/old.sh",
		Src:    "git:git@example.com:config.git#scripts/old.sh",
		SHA256: sha256Hex(oldFileContents),
	}
	if err := saveState(root, state); err != nil {
		t.Fatal(err)
	}

	out.Reset()
	if err := Status(StatusOptions{}, &out); err != nil {
		t.Fatal(err)
	}
	got = out.String()
	if !strings.Contains(got, "services/old") || !strings.Contains(got, "scripts/old.sh") {
		t.Fatalf("expected installed archived entries in status, got:\n%s", got)
	}

	out.Reset()
	if err := Status(StatusOptions{IncludeArchived: true}, &out); err != nil {
		t.Fatal(err)
	}
	got = out.String()
	if !strings.Contains(got, "services/old") || !strings.Contains(got, "scripts/old.sh") {
		t.Fatalf("expected archived entries with --archived, got:\n%s", got)
	}
}
