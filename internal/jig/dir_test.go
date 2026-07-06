package jig

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.email=t@t", "-c", "user.name=t"}, args...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestEnsureDirLifecycle(t *testing.T) {
	root := t.TempDir()
	t.Setenv("JIG_CACHE_DIR", filepath.Join(root, "cache"))
	remote := filepath.Join(root, "remote")
	if err := os.MkdirAll(filepath.Join(remote, "scripts", "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(rel, content string, mode os.FileMode) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(remote, rel), []byte(content), mode); err != nil {
			t.Fatal(err)
		}
	}
	write("scripts/dev.sh", "#!/bin/sh\necho dev\n", 0o755)
	write("scripts/sub/util.sh", "util v1\n", 0o644)
	write("scripts/gone.sh", "gone\n", 0o644)
	write("top.txt", "top\n", 0o644)
	gitIn(t, remote, "init", "-q")
	gitIn(t, remote, "add", ".")
	gitIn(t, remote, "commit", "-qm", "init")

	state := emptyState()
	model := Model{Entries: map[string]Entry{
		"tools/scripts": {Path: "tools/scripts", Identity: "scripts", Kind: EntryDir,
			Dir: &Dir{Src: "git:" + remote + "#scripts"}},
	}}
	ensure := func() string {
		var out bytes.Buffer
		if err := ensureDir(&out, root, &model, &state, "tools/scripts", true, false, newFileFetcher()); err != nil {
			t.Fatalf("ensureDir: %v", err)
		}
		return out.String()
	}

	// Initial materialization.
	if got := ensure(); !strings.Contains(got, "wrote-dir: tools/scripts (3 added)") {
		t.Fatalf("first run = %q", got)
	}
	devPath := filepath.Join(root, "tools", "scripts", "dev.sh")
	if info, err := os.Stat(devPath); err != nil || info.Mode().Perm() != 0o755 {
		t.Fatalf("dev.sh mode = %v, %v; want 0755", info, err)
	}
	if got := ensure(); !strings.Contains(got, "present-dir:") {
		t.Fatalf("second run = %q", got)
	}

	// Local modification survives an upstream update of another file, and a
	// file deleted upstream is removed locally when untouched.
	if err := os.WriteFile(filepath.Join(root, "tools", "scripts", "sub", "util.sh"), []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	write("scripts/dev.sh", "#!/bin/sh\necho dev v2\n", 0o755)
	write("scripts/sub/util.sh", "util v2\n", 0o644)
	write("scripts/new.sh", "new\n", 0o644)
	if err := os.Remove(filepath.Join(remote, "scripts", "gone.sh")); err != nil {
		t.Fatal(err)
	}
	gitIn(t, remote, "add", "-A")
	gitIn(t, remote, "commit", "-qm", "v2")

	got := ensure()
	for _, want := range []string{"updated-dir:", "1 added", "1 updated", "1 deleted", "1 modified kept"} {
		if !strings.Contains(got, want) {
			t.Fatalf("update run = %q, missing %q", got, want)
		}
	}
	if data, _ := os.ReadFile(devPath); string(data) != "#!/bin/sh\necho dev v2\n" {
		t.Fatalf("dev.sh = %q, want v2", data)
	}
	if data, _ := os.ReadFile(filepath.Join(root, "tools", "scripts", "sub", "util.sh")); string(data) != "edited\n" {
		t.Fatalf("util.sh = %q, want local edit kept", data)
	}
	if pathExists(filepath.Join(root, "tools", "scripts", "gone.sh")) {
		t.Fatal("expected gone.sh deleted")
	}
	if !pathExists(filepath.Join(root, "tools", "scripts", "new.sh")) {
		t.Fatal("expected new.sh written")
	}
}

func TestDirValidationAndWholeRepoSrc(t *testing.T) {
	def := testDefinition(t, `{
  "version": 1,
  "tree": {
    "tools/config": {
      "$dir": { "id": "config", "src": "git:git@example.com:config.git" }
    },
    "tools/bad": {
      "$dir": { "id": "bad", "src": "https://not-git" }
    }
  }
}`)
	result := validateDefinition(def)
	if len(result.Errors) != 1 || !strings.Contains(result.Errors[0], "tools/bad") {
		t.Fatalf("errors = %#v, want one about tools/bad", result.Errors)
	}
}
