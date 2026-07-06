package jig

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
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

	if err := ensureFile(ioDiscard{}, root, &model, &state, "bin/dev", true, false, newFileFetcher()); err != nil {
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

	err := ensureFile(ioDiscard{}, root, &model, &state, "scripts/dev.sh", true, false, newFileFetcher())
	if err == nil || err.Error() != "locally modified" {
		t.Fatalf("expected locally modified error, got %v", err)
	}
}

func TestEnsureFilePicksUpSourceChanges(t *testing.T) {
	root := t.TempDir()
	t.Setenv("JIG_CACHE_DIR", filepath.Join(root, "cache"))
	remote := filepath.Join(root, "remote")
	testRemoteRepo(t, remote)
	src := "git:" + remote + "#README.md"

	state := emptyState()
	model := Model{Entries: map[string]Entry{
		"docs/readme.md": testFileEntry("docs/readme.md", "readme", File{Src: src}),
	}}
	ensure := func() string {
		var out bytes.Buffer
		if err := ensureFile(&out, root, &model, &state, "docs/readme.md", true, false, newFileFetcher()); err != nil {
			t.Fatalf("ensureFile: %v", err)
		}
		return out.String()
	}

	if got := ensure(); !strings.Contains(got, "wrote-file:") {
		t.Fatalf("first run = %q, want wrote-file", got)
	}
	if state.Files["readme"].SrcBlob == "" {
		t.Fatal("expected srcBlob recorded")
	}
	if got := ensure(); !strings.Contains(got, "present-file:") {
		t.Fatalf("second run = %q, want present-file", got)
	}

	// Change the file upstream: sync must pick it up.
	if err := os.WriteFile(filepath.Join(remote, "README.md"), []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	commit := exec.Command("git", "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qam", "v2")
	commit.Dir = remote
	if out, err := commit.CombinedOutput(); err != nil {
		t.Fatalf("commit: %v\n%s", err, out)
	}
	if got := ensure(); !strings.Contains(got, "updated-file:") {
		t.Fatalf("after upstream change = %q, want updated-file", got)
	}
	data, err := os.ReadFile(filepath.Join(root, "docs", "readme.md"))
	if err != nil || string(data) != "v2\n" {
		t.Fatalf("content = %q, %v; want v2", data, err)
	}

	// A locally modified file is never overwritten, even when upstream moved.
	if err := os.WriteFile(filepath.Join(root, "docs", "readme.md"), []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err = ensureFile(ioDiscard{}, root, &model, &state, "docs/readme.md", true, false, newFileFetcher())
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
