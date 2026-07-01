package jig

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestFileLinkValidationAndOrdering(t *testing.T) {
	def := testDefinition(t, `{
  "version": 1,
  "tree": {
    "scripts/dev.sh": {
      "$file": {
        "id": "dev-script",
        "src": "git:git@example.com:config.git#scripts/dev.sh"
      }
    },
    "bin/dev": {
      "$file": {
        "id": "dev-command",
        "link": "scripts/dev.sh"
      }
    }
  }
}`)
	result := validateDefinition(def)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected validation errors: %#v", result.Errors)
	}
	model, err := flattenDefinition(def)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := resolvePlan(&model, []string{}, planOptions{})
	if err != nil {
		t.Fatal(err)
	}
	active := map[string]bool{"scripts/dev.sh": true, "bin/dev": true}
	ordered := orderFilesForApply(&model, active)
	want := []string{"scripts/dev.sh", "bin/dev"}
	if !reflect.DeepEqual(ordered, want) {
		t.Fatalf("ordered files = %#v, want %#v", ordered, want)
	}
	_ = plan
}

func TestEnsureLinkFileCreatesRelativeSymlink(t *testing.T) {
	root := t.TempDir()
	targetPath := filepath.Join(root, "scripts", "dev.sh")
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targetPath, []byte("script"), 0o644); err != nil {
		t.Fatal(err)
	}
	state := emptyState()
	model := Model{Entries: map[string]Entry{
		"scripts/dev.sh": testFileEntry("scripts/dev.sh", "dev-script", File{Src: "git:git@example.com:config.git#scripts/dev.sh"}),
		"bin/dev":        testFileEntry("bin/dev", "dev-command", File{Link: "scripts/dev.sh"}),
	}}

	if err := ensureFile(ioDiscard{}, root, &model, &state, "bin/dev", true); err != nil {
		t.Fatal(err)
	}
	target, err := os.Readlink(filepath.Join(root, "bin", "dev"))
	if err != nil {
		t.Fatal(err)
	}
	if target != "../scripts/dev.sh" {
		t.Fatalf("symlink target = %q", target)
	}
	if state.Files["dev-command"].Link != "scripts/dev.sh" {
		t.Fatalf("state = %#v", state.Files["dev-command"])
	}
}

func TestEnsureFilePreservesLocalModification(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "scripts", "dev.sh")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("edited"), 0o644); err != nil {
		t.Fatal(err)
	}
	state := State{Version: 1, Repos: map[string]StateRepo{}, Files: map[string]StateFile{
		"dev-script": {Path: "scripts/dev.sh", Src: "git:git@example.com:config.git#scripts/dev.sh", SHA256: sha256Hex([]byte("original"))},
	}}
	model := Model{Entries: map[string]Entry{
		"scripts/dev.sh": testFileEntry("scripts/dev.sh", "dev-script", File{Src: "git:git@example.com:config.git#scripts/dev.sh"}),
	}}

	err := ensureFile(ioDiscard{}, root, &model, &state, "scripts/dev.sh", true)
	if err == nil || err.Error() != "locally modified" {
		t.Fatalf("expected locally modified error, got %v", err)
	}
}

func TestInstalledFileIdentitySetRequiresTrackedExistingFile(t *testing.T) {
	root := t.TempDir()
	model := Model{
		Entries: map[string]Entry{
			"tracked.txt":   testFileEntry("tracked.txt", "tracked", File{Archived: true}),
			"untracked.txt": testFileEntry("untracked.txt", "untracked", File{Archived: true}),
		},
	}
	state := State{
		Version: 1,
		Repos:   map[string]StateRepo{},
		Files: map[string]StateFile{
			"tracked": {Path: "tracked.txt"},
		},
	}
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("tracked"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "untracked.txt"), []byte("untracked"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := installedFileIdentitySet(root, &model, &state)
	if !got["tracked"] {
		t.Fatalf("expected tracked file to be installed: %#v", got)
	}
	if got["untracked"] {
		t.Fatalf("did not expect untracked file to be installed: %#v", got)
	}
}
