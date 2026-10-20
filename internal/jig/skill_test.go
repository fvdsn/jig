package jig

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The jig skill is installed in every workspace, agents block or not,
// keyed by the binary's version so an upgrade rewrites it.
func TestEmbeddedSkillInstalledWithoutAgentsBlock(t *testing.T) {
	useEmbeddedSkill(t)
	model, err := flattenDefinition(testDefinition(t, `{"version": 3, "tree": {}}`), []string{"claude", "codex"})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(sortedEntryPaths(&model), " "); got != ".agents/skills .claude/skills" {
		t.Fatalf("entries = %s", got)
	}
	if entry := model.Entries[".agents/skills"]; entry.Dir.Src[0].describe() != "embedded:jig-skill" || len(entry.Dir.Src) != 1 {
		t.Fatalf("sources = %+v", entry.Dir.Src)
	}
	if errs := validateDefinition(testDefinition(t, `{"version": 3, "tree": {}}`)).Errors; len(errs) != 0 {
		t.Fatalf("errors = %v", errs)
	}

	root := t.TempDir()
	t.Setenv("JIG_CACHE_DIR", filepath.Join(root, "cache"))
	writeTestWorkspace(t, root, `{"version": 3, "tree": {}}`)
	harnesses := []string{"claude", "codex"}
	if err := saveConfig(root, Config{Version: 1, Schema: "jig.json", Harnesses: &harnesses}); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	var out bytes.Buffer
	if err := Clone(CloneOptions{}, &out); err != nil {
		t.Fatalf("clone: %v\n%s", err, out.String())
	}
	want, err := os.ReadFile(filepath.Join(repoSkills, "jig", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{".agents/skills/jig/SKILL.md", ".claude/skills/jig/SKILL.md"} {
		got, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("%s: %v\n%s", rel, err, out.String())
		}
	}
	state, err := loadState(root)
	if err != nil {
		t.Fatal(err)
	}
	if src := state.Dirs["agents-skills"].Src; src != "embedded:jig-skill@test" {
		t.Fatalf("state src = %q", src)
	}
	out.Reset()
	if err := Sync(SyncOptions{SkipUpdate: true}, &out); err != nil {
		t.Fatalf("sync: %v\n%s", err, out.String())
	}
	if strings.Contains(out.String(), "wrote-dir") || strings.Contains(out.String(), "updated-dir") {
		t.Fatalf("second sync not clean:\n%s", out.String())
	}

	// An upgraded binary: a new version with a changed skill rewrites it.
	upgraded := filepath.Join(root, "upgraded")
	if err := os.MkdirAll(filepath.Join(upgraded, "jig"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(upgraded, "jig", "SKILL.md"), []byte("---\nname: jig\n---\nnewer\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	EmbeddedSkill = os.DirFS(upgraded)
	EmbeddedSkillVersion = "test2"
	out.Reset()
	if err := Sync(SyncOptions{SkipUpdate: true}, &out); err != nil {
		t.Fatalf("sync upgrade: %v\n%s", err, out.String())
	}
	got, _ := os.ReadFile(filepath.Join(root, ".agents", "skills", "jig", "SKILL.md"))
	if !strings.Contains(string(got), "newer") || !strings.Contains(out.String(), "updated-dir: .agents/skills") {
		t.Fatalf("upgrade did not flow:\n%s", out.String())
	}
	if state, _ := loadState(root); state.Dirs["agents-skills"].Src != "embedded:jig-skill@test2" {
		t.Fatalf("state src = %q", state.Dirs["agents-skills"].Src)
	}
	if !strings.Contains(out.String(), "updated-dir: .claude/skills") {
		t.Fatalf("copy not refreshed:\n%s", out.String())
	}
}

// A schema that declares the skills directory itself takes the skill in
// as a source; its own skills still win, a declared copy is left alone,
// and an explicit jig skill in the block shadows the embedded one.
func TestEmbeddedSkillJoinsDeclaredSkillsDir(t *testing.T) {
	useEmbeddedSkill(t)
	root := t.TempDir()
	t.Setenv("JIG_CACHE_DIR", filepath.Join(root, "cache"))
	own := testSkillsSource(t, root, "own-skills", map[string]string{"shared/SKILL.md": "mine\n"})
	schema := fmt.Sprintf(`{"version": 3, "tree": {
	  ".agents/skills": {"$dir": {"id": "skills", "src": %q}},
	  ".claude/skills": {"$dir": {"id": "claude-skills", "copy": {"id": "skills"}}}
	}}`, own+"#skills")
	def := testDefinition(t, schema)
	if errs := validateDefinition(def).Errors; len(errs) != 0 {
		t.Fatalf("errors = %v", errs)
	}
	model, err := flattenDefinition(def, []string{"claude", "codex"})
	if err != nil {
		t.Fatal(err)
	}
	if entry := model.Entries[".agents/skills"]; entry.Identity != "skills" || len(entry.Dir.Src) != 2 || entry.Dir.Src[0].describe() != "embedded:jig-skill" {
		t.Fatalf("declared dir = %+v", entry.Dir)
	}
	if entry := model.Entries[".claude/skills"]; entry.Identity != "claude-skills" || entry.Dir.Copy == nil {
		t.Fatalf("declared copy = %+v", entry.Dir)
	}

	writeTestWorkspace(t, root, schema)
	harnesses := []string{"claude", "codex"}
	if err := saveConfig(root, Config{Version: 1, Schema: "jig.json", Harnesses: &harnesses}); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	var out bytes.Buffer
	if err := Clone(CloneOptions{}, &out); err != nil {
		t.Fatalf("clone: %v\n%s", err, out.String())
	}
	for _, rel := range []string{".agents/skills/jig/SKILL.md", ".agents/skills/shared/SKILL.md", ".claude/skills/jig/SKILL.md", ".claude/skills/shared/SKILL.md"} {
		if !pathExists(filepath.Join(root, filepath.FromSlash(rel))) {
			t.Fatalf("missing %s:\n%s", rel, out.String())
		}
	}

	// The block's own jig skill wins and the embedded one is reported
	// as shadowed.
	custom := testSkillsSource(t, root, "custom-skills", map[string]string{"jig/SKILL.md": "custom jig\n"})
	if err := os.WriteFile(filepath.Join(root, sourceDir, "jig.json"), []byte(fmt.Sprintf(`{"version": 4, "agents": {"skills": [%q]}, "tree": {}}`, custom+"#skills")), 0o644); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := Sync(SyncOptions{SkipUpdate: true, Prune: true}, &out); err != nil {
		t.Fatalf("sync: %v\n%s", err, out.String())
	}
	got, _ := os.ReadFile(filepath.Join(root, ".agents", "skills", "jig", "SKILL.md"))
	if string(got) != "custom jig\n" || !strings.Contains(out.String(), "shadowed") {
		t.Fatalf("explicit skill did not win:\n%s", out.String())
	}

	// With skills of its own, the block still collides with a declared
	// directory.
	_, err = flattenDefinition(testDefinition(t, fmt.Sprintf(`{"version": 4, "agents": {"skills": [%q]}, "tree": {".agents/skills": {"$dir": {"src": %q}}}}`, custom+"#skills", own+"#skills")), nil)
	if err == nil || !strings.Contains(err.Error(), "derives .agents/skills") {
		t.Fatalf("collision = %v", err)
	}
}
