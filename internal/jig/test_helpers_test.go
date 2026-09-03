package jig

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestMain points the repo cache at a throwaway directory so tests never
// touch the developer's real cache.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "jig-test-cache-*")
	if err != nil {
		panic(err)
	}
	if err := os.Setenv("JIG_CACHE_DIR", dir); err != nil {
		panic(err)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// writeTestWorkspace lays out a workspace at root with the given schema in
// its source directory and empty state.
func writeTestWorkspace(t *testing.T, root string, schema string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, sourceDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, sourceDir, "jig.json"), []byte(schema), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := saveConfig(root, Config{Version: 1, Schema: "jig.json"}); err != nil {
		t.Fatal(err)
	}
	if err := saveState(root, emptyState()); err != nil {
		t.Fatal(err)
	}
}

func testDefinition(t *testing.T, body string) *Definition {
	t.Helper()
	var def Definition
	if err := json.Unmarshal([]byte(body), &def); err != nil {
		t.Fatal(err)
	}
	return &def
}

func testRepoEntry(path, identity string, repo Repo) Entry {
	return Entry{Path: path, Identity: identity, Kind: EntryRepo, Repo: &repo}
}

func testFileEntry(path, identity string, file File) Entry {
	return Entry{Path: path, Identity: identity, Kind: EntryFile, File: &file}
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }

// gitIn runs a git command in dir and fails the test on error.
func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.email=t@t", "-c", "user.name=t"}, args...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// testCommitAll turns dir into a git repository with everything in it
// committed, the shape every source repository in the tests takes.
func testCommitAll(t *testing.T, dir string) {
	t.Helper()
	gitIn(t, dir, "init", "-q")
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-qm", "init")
}

// testSkillsSource creates a committed source repository under root whose
// files live in a skills/ subtree, the shape the $dir tests merge from.
func testSkillsSource(t *testing.T, root string, name string, files map[string]string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	nested := map[string]string{}
	for rel, content := range files {
		nested["skills/"+rel] = content
	}
	testFileSource(t, dir, nested)
	return dir
}

// testFileSource creates a committed source repository at dir holding the
// given files (relative path to content).
func testFileSource(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	testCommitAll(t, dir)
}
