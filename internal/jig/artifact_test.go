package jig

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A written entry whose schema path changed follows the move on sync and
// is refused on clone, for files and dirs alike.
func TestMaterializerFollowsSchemaMoves(t *testing.T) {
	root := t.TempDir()
	t.Setenv("JIG_CACHE_DIR", filepath.Join(root, "cache"))
	source := filepath.Join(root, "source")
	testFileSource(t, source, map[string]string{"notes.md": "notes\n", "skills/a.md": "a\n"})
	content := []byte("notes\n")

	model := Model{Entries: map[string]Entry{
		"docs/notes.md": testFileEntry("docs/notes.md", "notes", File{Src: SrcList{{Src: source + "#notes.md"}}}),
		".agents/skills": {Path: ".agents/skills", Identity: "skills", Kind: EntryDir,
			Dir: &Dir{Src: SrcList{{Src: source + "#skills"}}}},
	}}
	resolveLinkPaths(&model)

	// Both entries were written at their old paths.
	state := emptyState()
	if err := os.MkdirAll(filepath.Join(root, "old", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "old", "notes.md"), content, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "old", "skills", "a.md"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	state.Files["notes"] = StateFile{Path: "old/notes.md", Src: source + "#notes.md", SHA256: sha256Hex(content)}
	state.Dirs["skills"] = StateDir{Path: "old/skills", Src: source + "#skills", Tree: "stale", Files: map[string]string{"a.md": sha256Hex([]byte("a\n"))}}

	// clone refuses to move.
	m := newMaterializer(ioDiscard{}, root, &model, &state, newFileFetcher(), nil, nil, false)
	if err := m.ensureFile("docs/notes.md"); err == nil || !strings.Contains(err.Error(), "already written at old/notes.md") {
		t.Fatalf("clone move of a file: %v", err)
	}
	if err := m.ensureDir(".agents/skills"); err == nil || !strings.Contains(err.Error(), "already written at old/skills") {
		t.Fatalf("clone move of a dir: %v", err)
	}

	// sync moves, then converges at the new path.
	var out bytes.Buffer
	m = newMaterializer(&out, root, &model, &state, newFileFetcher(), nil, nil, true)
	if err := m.ensureFile("docs/notes.md"); err != nil {
		t.Fatalf("sync move of a file: %v", err)
	}
	if err := m.ensureDir(".agents/skills"); err != nil {
		t.Fatalf("sync move of a dir: %v", err)
	}
	got := out.String()
	for _, want := range []string{"moved-file: docs/notes.md: old/notes.md -> docs/notes.md", "moved-dir: .agents/skills: old/skills -> .agents/skills"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output = %q, want %q", got, want)
		}
	}
	if !pathExists(filepath.Join(root, "docs", "notes.md")) || pathExists(filepath.Join(root, "old")) {
		t.Fatal("expected the entries at their new paths and the old tree pruned")
	}
	if state.Files["notes"].Path != "docs/notes.md" || state.Dirs["skills"].Path != ".agents/skills" {
		t.Fatalf("state paths = %q, %q; want the new paths", state.Files["notes"].Path, state.Dirs["skills"].Path)
	}
}

// A path jig did not write is never overwritten: an untracked file, a
// directory where a file is expected, and a foreign symlink each fail the
// entry.
func TestMaterializerRefusesForeignPaths(t *testing.T) {
	root := t.TempDir()
	t.Setenv("JIG_CACHE_DIR", filepath.Join(root, "cache"))
	source := filepath.Join(root, "source")
	testFileSource(t, source, map[string]string{"notes.md": "notes\n"})
	model := Model{Entries: map[string]Entry{
		"a.md": testFileEntry("a.md", "a", File{Src: SrcList{{Src: source + "#notes.md"}}}),
		"b.md": testFileEntry("b.md", "b", File{Src: SrcList{{Src: source + "#notes.md"}}}),
		"c.md": testFileEntry("c.md", "c", File{Src: SrcList{{Src: source + "#notes.md"}}}),
	}}
	resolveLinkPaths(&model)
	if err := os.WriteFile(filepath.Join(root, "a.md"), []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "b.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("a.md", filepath.Join(root, "c.md")); err != nil {
		t.Fatal(err)
	}
	state := emptyState()
	m := newMaterializer(ioDiscard{}, root, &model, &state, newFileFetcher(), nil, nil, true)
	for path, want := range map[string]string{
		"a.md": "existing file is not tracked",
		"b.md": "expected file path is a directory",
		"c.md": "existing path is a symlink",
	} {
		if err := m.ensureFile(path); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("ensureFile(%s) = %v, want %q", path, err, want)
		}
	}
}
