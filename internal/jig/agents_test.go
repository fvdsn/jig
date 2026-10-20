package jig

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const agentsTestSchema = `{
  "version": 4,
  "agents": {
    "skills": ["git@example.com:meta.git#skills"],
    "context": ["git@example.com:meta.git#context/workspace.md"]
  },
  "tree": {}
}`

// The block derives the canonical entries always and an alias per listed
// harness that reads elsewhere, with fixed identities and resolved alias
// targets.
func TestAgentsBlockExpandsForHarnesses(t *testing.T) {
	model, err := flattenDefinition(testDefinition(t, agentsTestSchema), []string{"copilot", "claude"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]EntryKind{
		".agents/skills": EntryDir, ".claude/skills": EntryDir,
		"AGENTS.md": EntryFile, "CLAUDE.md": EntryFile, ".github/copilot-instructions.md": EntryFile,
	}
	if len(model.Entries) != len(want) {
		t.Fatalf("entries = %v", sortedEntryPaths(&model))
	}
	for path, kind := range want {
		entry, ok := model.Entries[path]
		if !ok || entry.Kind != kind {
			t.Fatalf("missing %s %s in %v", kind, path, sortedEntryPaths(&model))
		}
	}
	skills := model.Entries[".agents/skills"]
	if skills.Identity != "agents-skills" || len(skills.Dir.Src) != 1 || !skills.Dir.skills {
		t.Fatalf("skills entry = %+v", skills.Dir)
	}
	claudeSkills := model.Entries[".claude/skills"]
	if claudeSkills.Identity != "agents-skills-claude" || claudeSkills.Dir.Copy == nil || claudeSkills.Dir.copyPath != ".agents/skills" {
		t.Fatalf("claude skills alias = %+v", claudeSkills.Dir)
	}
	context := model.Entries["AGENTS.md"]
	if context.Identity != "agents-context" || len(context.File.Src) != 1 {
		t.Fatalf("context entry = %+v", context.File)
	}
	for _, path := range []string{"CLAUDE.md", ".github/copilot-instructions.md"} {
		alias := model.Entries[path]
		if alias.File.Link == nil || alias.File.linkPath != "AGENTS.md" {
			t.Fatalf("%s = %+v, want a link to AGENTS.md", path, alias.File)
		}
	}
	if len(skills.Tags) != 0 || skills.Meta != nil || skills.Dir.Archived {
		t.Fatalf("derived entries carry no tags, meta, or archived: %+v", skills)
	}

	// A harness that reads only its own paths gets the real entries there
	// and no canonical copy for its search tools to find twice.
	model, err = flattenDefinition(testDefinition(t, agentsTestSchema), []string{"claude"})
	if err != nil {
		t.Fatal(err)
	}
	if got := sortedEntryPaths(&model); strings.Join(got, " ") != ".claude/skills CLAUDE.md" {
		t.Fatalf("claude-only entries = %v", got)
	}
	if own := model.Entries[".claude/skills"]; own.Identity != "agents-skills-claude" || len(own.Dir.Src) != 1 || own.Dir.Copy != nil || !own.Dir.skills {
		t.Fatalf("claude-only skills = %+v", own.Dir)
	}
	if own := model.Entries["CLAUDE.md"]; len(own.File.Src) != 1 || own.File.Link != nil {
		t.Fatalf("claude-only context = %+v", own.File)
	}
	// Two such harnesses each get their own real entry.
	model, err = flattenDefinition(testDefinition(t, agentsTestSchema), []string{"kiro", "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if got := sortedEntryPaths(&model); strings.Join(got, " ") != ".claude/skills .kiro/skills AGENTS.md CLAUDE.md" {
		t.Fatalf("claude+kiro entries = %v", got)
	}
	if kiro := model.Entries[".kiro/skills"]; len(kiro.Dir.Src) != 1 || kiro.Dir.Copy != nil {
		t.Fatalf("kiro skills = %+v", kiro.Dir)
	}

	// No harness derives the canonical entries only.
	model, err = flattenDefinition(testDefinition(t, agentsTestSchema), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := sortedEntryPaths(&model); strings.Join(got, " ") != ".agents/skills AGENTS.md" {
		t.Fatalf("canonical-only entries = %v", got)
	}

	// A primitive without sources derives nothing, aliases included.
	model, err = flattenDefinition(testDefinition(t, `{"version": 4, "agents": {"context": ["git@example.com:meta.git#AGENTS.md"]}, "tree": {}}`), []string{"claude"})
	if err != nil {
		t.Fatal(err)
	}
	if got := sortedEntryPaths(&model); strings.Join(got, " ") != "CLAUDE.md" {
		t.Fatalf("context-only entries = %v", got)
	}
}

// The tree can link to the derived entries by their fixed identities,
// which is how a harness outside the matrix gets its alias.
func TestAgentsBlockEntriesAreLinkable(t *testing.T) {
	model, err := flattenDefinition(testDefinition(t, `{
  "version": 4,
  "agents": {"skills": ["git@example.com:meta.git#skills"]},
  "tree": {".opencode/skills": {"$dir": {"link": {"id": "agents-skills"}}}}
}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if link := model.Entries[".opencode/skills"]; link.Dir.linkPath != ".agents/skills" {
		t.Fatalf("link target = %q", link.Dir.linkPath)
	}
	if errs := validateDefinition(testDefinition(t, `{
  "version": 4,
  "agents": {"skills": ["git@example.com:meta.git#skills"]},
  "tree": {".opencode/skills": {"$dir": {"link": {"id": "agents-skills"}}}}
}`)).Errors; len(errs) != 0 {
		t.Fatalf("validation errors = %v", errs)
	}
}

// The explicit form and the block cannot both own a path, and validation
// catches a collision with any harness's alias, whatever this machine
// uses.
func TestAgentsBlockRejectsPathsTheTreeDeclares(t *testing.T) {
	_, err := flattenDefinition(testDefinition(t, `{
  "version": 4,
  "agents": {"skills": ["git@example.com:meta.git#skills"]},
  "tree": {".agents/skills": {"$dir": {"src": "git@example.com:other.git#skills"}}}
}`), nil)
	if err == nil || !strings.Contains(err.Error(), "derives .agents/skills, which the tree also declares") {
		t.Fatalf("collision error = %v", err)
	}

	result := validateDefinition(testDefinition(t, `{
  "version": 4,
  "agents": {"context": ["git@example.com:meta.git#AGENTS.md"]},
  "tree": {"GEMINI.md": {"$file": {"src": "git@example.com:other.git#GEMINI.md"}}}
}`))
	if len(result.Errors) != 1 || !strings.Contains(result.Errors[0], "derives GEMINI.md") {
		t.Fatalf("validation errors = %v", result.Errors)
	}
}

func TestAgentsBlockRequiresVersion4(t *testing.T) {
	def := testDefinition(t, `{"version": 3, "agents": {"skills": ["git@example.com:meta.git#skills"]}, "tree": {}}`)
	if _, err := flattenDefinition(def, nil); err == nil || !strings.Contains(err.Error(), "requires version 4") {
		t.Fatalf("version guard = %v", err)
	}
	if errs := validateDefinition(def).Errors; len(errs) != 1 || !strings.Contains(errs[0], "requires version 4") {
		t.Fatalf("validation errors = %v", errs)
	}
	// Version 3 without the block and version 4 without it both still load.
	for _, body := range []string{`{"version": 3, "tree": {}}`, `{"version": 4, "tree": {}}`} {
		if errs := validateDefinition(testDefinition(t, body)).Errors; len(errs) != 0 {
			t.Fatalf("%s: %v", body, errs)
		}
	}
}

// Spec'd primitives this jig does not implement are refused by name, and
// so are unknown fields, so a schema never silently loses declarations.
func TestAgentsBlockRejectsUnimplementedAndUnknownFields(t *testing.T) {
	var def Definition
	err := json.Unmarshal([]byte(`{"version": 4, "agents": {"hooks": []}, "tree": {}}`), &def)
	if err == nil || !strings.Contains(err.Error(), "agents.hooks is not supported by this jig yet") {
		t.Fatalf("planned field = %v", err)
	}
	err = json.Unmarshal([]byte(`{"version": 4, "agents": {"skilz": []}, "tree": {}}`), &def)
	if err == nil || !strings.Contains(err.Error(), `unknown agents field "skilz"`) {
		t.Fatalf("unknown field = %v", err)
	}
	err = json.Unmarshal([]byte(`{"version": 4, "agents": {"skills": [{"file": "~/x"}]}, "tree": {}}`), &def)
	if err != nil {
		t.Fatal(err)
	}
	// A file source in the skills list is caught by the ordinary dir
	// source rules.
	if errs := validateDefinition(&def).Errors; len(errs) != 1 || !strings.Contains(errs[0], "dir .agents/skills invalid src: file is not valid here; use dir") {
		t.Fatalf("validation errors = %v", errs)
	}
}

// End to end: a Claude-only workspace materializes the skills and context
// at Claude's paths and nothing canonical; switching the harness list
// makes those stale and derives the canonical entries instead.
func TestAgentsBlockMaterializesForConfiguredHarnesses(t *testing.T) {
	root := t.TempDir()
	t.Setenv("JIG_CACHE_DIR", filepath.Join(root, "cache"))
	meta := filepath.Join(root, "meta")
	testFileSource(t, meta, map[string]string{
		"skills/review/SKILL.md": "review\n",
		"context/workspace.md":   "# workspace\n",
	})
	writeTestWorkspace(t, root, fmt.Sprintf(`{
  "version": 4,
  "agents": {
    "skills": [%q],
    "context": [%q]
  },
  "tree": {}
}`, meta+"#skills", meta+"#context/workspace.md"))
	claude := []string{"claude"}
	if err := saveConfig(root, Config{Version: 1, Schema: "jig.json", Harnesses: &claude}); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	var out bytes.Buffer
	if err := Clone(CloneOptions{}, &out); err != nil {
		t.Fatalf("clone: %v\n%s", err, out.String())
	}
	for _, rel := range []string{".claude/skills/review/SKILL.md", "CLAUDE.md"} {
		if !pathExists(filepath.Join(root, filepath.FromSlash(rel))) {
			t.Fatalf("expected %s, got:\n%s", rel, out.String())
		}
	}
	if isSymlink(filepath.Join(root, "CLAUDE.md")) || isSymlink(filepath.Join(root, ".claude", "skills")) {
		t.Fatalf("CLAUDE.md and .claude/skills must be real on a Claude-only machine:\n%s", out.String())
	}
	for _, rel := range []string{".agents", "AGENTS.md", "GEMINI.md"} {
		if pathExists(filepath.Join(root, rel)) {
			t.Fatalf("%s derived although nothing configured reads it:\n%s", rel, out.String())
		}
	}

	// Adding a harness that reads the canonical locations derives them and
	// turns the Claude entries into aliases, in place. (Clone rather than
	// sync: with no repository installed nothing activates root files,
	// and sync never materializes what was not cloned.)
	both := []string{"claude", "codex"}
	if err := saveConfig(root, Config{Version: 1, Schema: "jig.json", Harnesses: &both}); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := Clone(CloneOptions{}, &out); err != nil {
		t.Fatalf("clone with codex: %v\n%s", err, out.String())
	}
	if !pathExists(filepath.Join(root, ".agents", "skills", "review", "SKILL.md")) || !pathExists(filepath.Join(root, "AGENTS.md")) {
		t.Fatalf("canonical entries not derived for codex:\n%s", out.String())
	}
	if !isSymlink(filepath.Join(root, "CLAUDE.md")) || isSymlink(filepath.Join(root, ".claude", "skills")) {
		t.Fatalf("CLAUDE.md must now be a link and .claude/skills a copy:\n%s", out.String())
	}
	if data, _ := os.ReadFile(filepath.Join(root, ".claude", "skills", "review", "SKILL.md")); string(data) != "review\n" {
		t.Fatalf("copied skill = %q", data)
	}

	// Dropping both harnesses leaves the Claude entries stale; prune
	// removes them and the canonical entries stay for whatever comes.
	none := []string{}
	if err := saveConfig(root, Config{Version: 1, Schema: "jig.json", Harnesses: &none}); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := Sync(SyncOptions{SkipUpdate: true, Prune: true}, &out); err != nil {
		t.Fatalf("sync: %v\n%s", err, out.String())
	}
	if pathExists(filepath.Join(root, "CLAUDE.md")) || pathExists(filepath.Join(root, ".claude")) {
		t.Fatalf("claude entries survived the prune:\n%s", out.String())
	}
	if !pathExists(filepath.Join(root, ".agents", "skills", "review", "SKILL.md")) || !pathExists(filepath.Join(root, "AGENTS.md")) {
		t.Fatalf("canonical entries were removed:\n%s", out.String())
	}
}
