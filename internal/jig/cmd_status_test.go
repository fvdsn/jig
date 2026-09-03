package jig

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Stale entries are listed among the defined ones by path, in a stable
// order, rather than appended in map order.
func TestStatusMergesStaleEntriesByPath(t *testing.T) {
	root := t.TempDir()
	writeTestWorkspace(t, root, `{
  "version": 3,
  "tree": {
    "b/repo": { "$repo": { "git": "git@example.com:repo.git" } }
  }
}`)
	state := emptyState()
	state.Repos["b/repo"] = StateRepo{Path: "b/repo", Git: "git@example.com:repo.git"}
	for _, rel := range []string{"a/stale.txt", "c/stale.txt"} {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("stale"), 0o644); err != nil {
			t.Fatal(err)
		}
		state.Files[rel] = StateFile{Path: rel, SHA256: sha256Hex([]byte("stale"))}
	}
	if err := saveState(root, state); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	var first string
	for i := 0; i < 5; i++ {
		var out bytes.Buffer
		if err := Status(StatusOptions{}, &out); err != nil {
			t.Fatal(err)
		}
		got := out.String()
		a, b, c := strings.Index(got, "a/stale.txt"), strings.Index(got, "b/repo"), strings.Index(got, "c/stale.txt")
		if a < 0 || b < 0 || c < 0 || !(a < b && b < c) {
			t.Fatalf("expected a/stale.txt, b/repo, c/stale.txt in path order, got:\n%s", got)
		}
		if first == "" {
			first = got
		} else if got != first {
			t.Fatalf("status output changed between runs:\n%s\n---\n%s", first, got)
		}
	}
}

func TestStatusSkipsArchivedMissingEntriesUnlessIncluded(t *testing.T) {
	root := t.TempDir()
	writeTestWorkspace(t, root, `{
  "version": 3,
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

	t.Chdir(root)

	var out bytes.Buffer
	if err := Status(StatusOptions{}, &out); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	// Uninstalled repos are only counted by default.
	if strings.Contains(got, "services/current") {
		t.Fatalf("did not expect uninstalled repo in status, got:\n%s", got)
	}
	if !strings.Contains(got, "not installed") {
		t.Fatalf("expected not-installed count in summary, got:\n%s", got)
	}

	out.Reset()
	if err := Status(StatusOptions{All: true}, &out); err != nil {
		t.Fatal(err)
	}
	got = out.String()
	if !strings.Contains(got, "services/current") {
		t.Fatalf("expected current repo in status --all, got:\n%s", got)
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
