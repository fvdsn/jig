package jig

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sampleRule = "---\ndescription: How to write database migrations\npaths:\n  - \"**/migrations/*.sql\"\n  - \"db/**/*.sql\"\n---\n# SQL migrations\n\n- Ship the down migration in the same file.\n"

func TestRuleTransforms(t *testing.T) {
	body := "# SQL migrations\n\n- Ship the down migration in the same file.\n"
	cases := []struct {
		transform string
		rel       string
		content   string
	}{
		{"claude-rule", "sql-migrations.md", "---\npaths:\n  - \"**/migrations/*.sql\"\n  - \"db/**/*.sql\"\n---\n" + body},
		{"copilot-rule", "sql-migrations.instructions.md", "---\ndescription: \"How to write database migrations\"\napplyTo: \"**/migrations/*.sql,db/**/*.sql\"\n---\n" + body},
		{"cursor-rule", "sql-migrations.mdc", "---\ndescription: \"How to write database migrations\"\nglobs: \"**/migrations/*.sql,db/**/*.sql\"\nalwaysApply: false\n---\n" + body},
		{"windsurf-rule", "sql-migrations.md", "---\ntrigger: glob\nglobs:\n  - \"**/migrations/*.sql\"\n  - \"db/**/*.sql\"\n---\n" + body},
		{"kiro-rule", "sql-migrations.md", "---\ninclusion: fileMatch\nfileMatchPattern:\n  - \"**/migrations/*.sql\"\n  - \"db/**/*.sql\"\n---\n" + body},
	}
	for _, c := range cases {
		// The APM naming convention is stripped from the stem.
		rel, out := dirTransforms[c.transform]("sql-migrations.instructions.md", []byte(sampleRule))
		if rel != c.rel || string(out) != c.content {
			t.Fatalf("%s: %s\n%s\nwant %s\n%s", c.transform, rel, out, c.rel, c.content)
		}
	}

	// An always-on rule, written with applyTo as a comma list, and a rule
	// with no frontmatter at all.
	always := []byte("---\napplyTo: \"**/*.py, **/*.{pyi,pyx}\"\n---\nUse pathlib.\n")
	if _, out := claudeRuleTransform("py.md", always); string(out) != "---\npaths:\n  - \"**/*.py\"\n  - \"**/*.{pyi,pyx}\"\n---\nUse pathlib.\n" {
		t.Fatalf("claude applyTo =\n%s", out)
	}
	plain := []byte("Be terse.\n")
	if rel, out := claudeRuleTransform("terse.md", plain); rel != "terse.md" || string(out) != "Be terse.\n" {
		t.Fatalf("claude plain = %s %q", rel, out)
	}
	if _, out := copilotRuleTransform("terse.md", plain); string(out) != "---\napplyTo: \"**\"\n---\nBe terse.\n" {
		t.Fatalf("copilot plain =\n%s", out)
	}
	if _, out := cursorRuleTransform("terse.md", plain); string(out) != "---\ndescription: \"Be terse\"\nalwaysApply: true\n---\nBe terse.\n" {
		t.Fatalf("cursor plain =\n%s", out)
	}
	if _, out := windsurfRuleTransform("terse.md", plain); string(out) != "---\ntrigger: always_on\n---\nBe terse.\n" {
		t.Fatalf("windsurf plain =\n%s", out)
	}
	if _, out := kiroRuleTransform("terse.md", plain); string(out) != "---\ninclusion: always\n---\nBe terse.\n" {
		t.Fatalf("kiro plain =\n%s", out)
	}
	if rel, out := kiroRuleTransform("notes.txt", plain); rel != "notes.txt" || !bytes.Equal(out, plain) {
		t.Fatal("non-markdown must pass through")
	}
	if got := splitGlobs("**/*.{css,scss}, src/**"); strings.Join(got, "|") != "**/*.{css,scss}|src/**" {
		t.Fatalf("splitGlobs = %v", got)
	}
}

func TestRenderFold(t *testing.T) {
	got := string(renderFold([]ruleFile{
		{rel: "sql-migrations.md", content: []byte(sampleRule)},
		{rel: "terse.md", content: []byte("Be terse.\n")},
	}))
	want := "\n## Instructions\n\n### How to write database migrations\n\nApplies to: `**/migrations/*.sql`, `db/**/*.sql`\n\n# SQL migrations\n\n- Ship the down migration in the same file.\n\n### terse\n\nBe terse.\n"
	if got != want {
		t.Fatalf("fold =\n%s\nwant\n%s", got, want)
	}
	if renderFold(nil) != nil {
		t.Fatal("no rules, no section")
	}
}

const agentsRulesSchema = `{
  "version": 4,
  "agents": {
    "context": ["git@example.com:meta.git#context/workspace.md"],
    "instructions": ["git@example.com:meta.git#instructions"]
  },
  "tree": {}
}`

// Root context files fold the instructions when a harness reading them
// has no per-file rules; an alias whose folding differs from the
// canonical file's becomes a real file instead of a link.
func TestAgentsBlockExpandsInstructions(t *testing.T) {
	expand := func(harnesses ...string) Model {
		t.Helper()
		model, err := flattenDefinition(testDefinition(t, agentsRulesSchema), harnesses)
		if err != nil {
			t.Fatal(err)
		}
		return model
	}
	paths := func(model Model) string { return strings.Join(sortedEntryPaths(&model), " ") }

	// Claude has per-file rules: CLAUDE.md stays unfolded.
	model := expand("claude")
	if paths(model) != ".claude/rules CLAUDE.md" {
		t.Fatalf("claude = %s", paths(model))
	}
	if rules := model.Entries[".claude/rules"]; rules.Dir.transform != "claude-rule" || !rules.Dir.items || len(rules.Dir.Src) != 1 {
		t.Fatalf("claude rules dir = %+v", rules.Dir)
	}
	if claude := model.Entries["CLAUDE.md"]; claude.File.rules != nil || claude.File.Link != nil {
		t.Fatalf("CLAUDE.md = %+v", claude.File)
	}

	// Codex folds and Copilot does not, and both read AGENTS.md: the shared
	// file folds (Copilot sees the rules twice, as with APM) while
	// Copilot's own root file is a real unfolded file.
	model = expand("codex", "copilot")
	if paths(model) != ".github/copilot-instructions.md .github/instructions AGENTS.md" {
		t.Fatalf("codex+copilot = %s", paths(model))
	}
	if agents := model.Entries["AGENTS.md"]; agents.File.rules == nil || len(agents.File.Src) != 1 {
		t.Fatalf("AGENTS.md = %+v", agents.File)
	}
	if copilot := model.Entries[".github/copilot-instructions.md"]; copilot.File.rules != nil || copilot.File.Link != nil || len(copilot.File.Src) != 1 {
		t.Fatalf("copilot root = %+v", copilot.File)
	}

	// Copilot alone: nothing folds, so its root file is a link again.
	model = expand("copilot")
	if copilot := model.Entries[".github/copilot-instructions.md"]; copilot.File.Link == nil || model.Entries["AGENTS.md"].File.rules != nil {
		t.Fatalf("copilot alone = %+v", copilot.File)
	}

	// Gemini reads only GEMINI.md and folds: one real folded file.
	model = expand("gemini")
	if paths(model) != "GEMINI.md" || model.Entries["GEMINI.md"].File.rules == nil {
		t.Fatalf("gemini = %s %+v", paths(model), model.Entries["GEMINI.md"].File)
	}

	// Cursor and Windsurf read AGENTS.md and have per-file rules.
	model = expand("cursor", "windsurf")
	if paths(model) != ".cursor/rules .windsurf/rules AGENTS.md" || model.Entries["AGENTS.md"].File.rules != nil {
		t.Fatalf("cursor+windsurf = %s", paths(model))
	}

	// No harness: the canonical files, with the unknown consumer served
	// the folded form.
	model = expand()
	if paths(model) != ".agents/rules AGENTS.md" || model.Entries["AGENTS.md"].File.rules == nil || model.Entries[".agents/rules"].Dir.transform != "" {
		t.Fatalf("none = %s", paths(model))
	}

	// Without instructions nothing folds and aliases stay links.
	model, err := flattenDefinition(testDefinition(t, agentsTestSchema), []string{"claude", "codex"})
	if err != nil {
		t.Fatal(err)
	}
	if claude := model.Entries["CLAUDE.md"]; claude.File.Link == nil || model.Entries["AGENTS.md"].File.rules != nil {
		t.Fatalf("no instructions = %+v", claude.File)
	}

	if errs := validateDefinition(testDefinition(t, agentsRulesSchema)).Errors; len(errs) != 0 {
		t.Fatalf("validation errors = %v", errs)
	}

	// Derived entries compare equal across loads, so a schema update does
	// not report them as changed.
	var out bytes.Buffer
	printDefinitionChanges(&out, &model, &model)
	again, _ := flattenDefinition(testDefinition(t, agentsTestSchema), []string{"claude", "codex"})
	printDefinitionChanges(&out, &model, &again)
	full, _ := flattenDefinition(testDefinition(t, agentsItemsSchema), []string{"codex", "gemini"})
	fullAgain, _ := flattenDefinition(testDefinition(t, agentsItemsSchema), []string{"codex", "gemini"})
	printDefinitionChanges(&out, &full, &fullAgain)
	if out.Len() != 0 {
		t.Fatalf("unchanged models reported changes:\n%s", out.String())
	}
}

// End to end: Codex gets the rules folded into AGENTS.md, Claude gets
// per-file rules and an unfolded CLAUDE.md, and a rule change upstream
// flows into both on sync.
func TestAgentsBlockMaterializesInstructions(t *testing.T) {
	root := t.TempDir()
	t.Setenv("JIG_CACHE_DIR", filepath.Join(root, "cache"))
	meta := filepath.Join(root, "meta")
	testFileSource(t, meta, map[string]string{
		"context/workspace.md":           "# Workspace\n\nContext here.\n",
		"instructions/sql-migrations.md": sampleRule,
		"instructions/terse.md":          "Be terse.\n",
		"instructions/README.txt":        "not a rule\n",
	})
	writeTestWorkspace(t, root, fmt.Sprintf(`{
  "version": 4,
  "agents": {
    "context": [%q],
    "instructions": [%q]
  },
  "tree": {}
}`, meta+"#context/workspace.md", meta+"#instructions"))
	harnesses := []string{"claude", "codex"}
	if err := saveConfig(root, Config{Version: 1, Schema: "jig.json", Harnesses: &harnesses}); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	var out bytes.Buffer
	if err := Clone(CloneOptions{}, &out); err != nil {
		t.Fatalf("clone: %v\n%s", err, out.String())
	}
	read := func(rel string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("%s: %v\n%s", rel, err, out.String())
		}
		return string(data)
	}
	agents := read("AGENTS.md")
	if !strings.HasPrefix(agents, "# Workspace\n\nContext here.\n") || !strings.Contains(agents, "\n## Instructions\n") ||
		!strings.Contains(agents, "### How to write database migrations\n\nApplies to: `**/migrations/*.sql`, `db/**/*.sql`\n") ||
		!strings.Contains(agents, "### terse\n\nBe terse.\n") || strings.Contains(agents, "not a rule") {
		t.Fatalf("AGENTS.md =\n%s", agents)
	}
	if claude := read("CLAUDE.md"); claude != "# Workspace\n\nContext here.\n" || isSymlink(filepath.Join(root, "CLAUDE.md")) {
		t.Fatalf("CLAUDE.md = %q", claude)
	}
	if got := read(".claude/rules/sql-migrations.md"); !strings.HasPrefix(got, "---\npaths:\n") {
		t.Fatalf(".claude/rules =\n%s", got)
	}
	read(".claude/rules/README.txt")
	for _, rel := range []string{".agents", ".codex/rules", ".codex"} {
		if pathExists(filepath.Join(root, rel)) {
			t.Fatalf("%s derived although nothing reads it", rel)
		}
	}

	out.Reset()
	if err := Sync(SyncOptions{SkipUpdate: true}, &out); err != nil {
		t.Fatalf("sync: %v\n%s", err, out.String())
	}
	if strings.Count(out.String(), "present-file:") != 2 || strings.Count(out.String(), "present-dir:") != 1 {
		t.Fatalf("second run:\n%s", out.String())
	}

	if err := os.WriteFile(filepath.Join(meta, "instructions", "terse.md"), []byte("Be very terse.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, meta, "commit", "-qam", "v2")
	out.Reset()
	if err := Sync(SyncOptions{SkipUpdate: true}, &out); err != nil {
		t.Fatalf("sync v2: %v\n%s", err, out.String())
	}
	if !strings.Contains(read("AGENTS.md"), "Be very terse.") || !strings.Contains(read(".claude/rules/terse.md"), "Be very terse.") {
		t.Fatalf("rule update did not flow:\n%s", out.String())
	}
	if strings.Contains(read("CLAUDE.md"), "terse") {
		t.Fatal("CLAUDE.md folded")
	}
}
