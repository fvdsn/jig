package jig

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const agentsItemsSchema = `{
  "version": 4,
  "agents": {
    "subagents": ["git@example.com:meta.git#agents/reviewer.md", "git@example.com:meta.git#agents"],
    "commands": ["git@example.com:meta.git#commands"]
  },
  "tree": {}
}`

// Subagents and commands have no shared location a harness reads, so each
// listed harness gets a real directory in its own format; harnesses
// without the primitive get nothing, and no harness at all derives the
// canonical directories.
func TestAgentsBlockExpandsSubagentsAndCommands(t *testing.T) {
	model, err := flattenDefinition(testDefinition(t, agentsItemsSchema), []string{"gemini", "codex", "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if got := sortedEntryPaths(&model); strings.Join(got, " ") != ".claude/agents .claude/commands .codex/agents .gemini/commands" {
		t.Fatalf("entries = %v", got)
	}
	claude := model.Entries[".claude/agents"]
	if claude.Identity != "agents-subagents-claude" || len(claude.Dir.Src) != 2 || claude.Dir.Copy != nil || !claude.Dir.items || claude.Dir.transform != "" {
		t.Fatalf("claude subagents = %+v", claude.Dir)
	}
	if codex := model.Entries[".codex/agents"]; codex.Dir.transform == "" || len(codex.Dir.Src) != 2 {
		t.Fatalf("codex subagents = %+v", codex.Dir)
	}
	if gemini := model.Entries[".gemini/commands"]; gemini.Dir.transform == "" || gemini.Identity != "agents-commands-gemini" {
		t.Fatalf("gemini commands = %+v", gemini.Dir)
	}

	// Windsurf reads commands as workflows and has no subagents.
	model, err = flattenDefinition(testDefinition(t, agentsItemsSchema), []string{"windsurf"})
	if err != nil {
		t.Fatal(err)
	}
	if got := sortedEntryPaths(&model); strings.Join(got, " ") != ".windsurf/workflows" {
		t.Fatalf("windsurf entries = %v", got)
	}

	model, err = flattenDefinition(testDefinition(t, agentsItemsSchema), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := sortedEntryPaths(&model); strings.Join(got, " ") != ".agents/agents .agents/commands" {
		t.Fatalf("canonical entries = %v", got)
	}
	if errs := validateDefinition(testDefinition(t, agentsItemsSchema)).Errors; len(errs) != 0 {
		t.Fatalf("validation errors = %v", errs)
	}

	// Both local forms are accepted, and a forge file URL parses.
	def := testDefinition(t, `{"version": 4, "agents": {"commands": [
	  {"file": "~/.config/acme/deploy.md"}, {"dir": "~/.config/acme/commands"},
	  "https://github.com/acme/meta/blob/main/commands/review.md",
	  {"file": "relative/path.md"}
	]}, "tree": {}}`)
	errs := validateDefinition(def).Errors
	if len(errs) != 1 || !strings.Contains(errs[0], "local path must start with ~/ or /") {
		t.Fatalf("validation errors = %v", errs)
	}
}

func TestSplitFrontmatter(t *testing.T) {
	fm, body := splitFrontmatter([]byte("---\nname: api-architect\ndescription: \"Designs: APIs\"\nmodel: 'opus'\ntools:\n  - Read\n  - Grep\nnotes: |\n  line one\n  line two\n---\n# Body\n\nText $ARGUMENTS\n"))
	if fm.get("name") != "api-architect" || fm.get("description") != "Designs: APIs" || fm.get("model") != "opus" {
		t.Fatalf("fields = %#v", fm.fields)
	}
	if fm.get("tools") != "Read, Grep" || fm.get("notes") != "line one\nline two" {
		t.Fatalf("list/block = %#v", fm.fields)
	}
	if strings.Join(fm.order, ",") != "name,description,model,tools,notes" {
		t.Fatalf("order = %v", fm.order)
	}
	if body != "# Body\n\nText $ARGUMENTS\n" {
		t.Fatalf("body = %q", body)
	}
	fm, body = splitFrontmatter([]byte("# No header\n"))
	if len(fm.fields) != 0 || body != "# No header\n" {
		t.Fatalf("headerless = %#v %q", fm.fields, body)
	}
}

func TestAgentTransforms(t *testing.T) {
	source := []byte("---\nname: api-architect\ndescription: Designs \"good\" APIs\nmodel: gpt-5\ntools: [Read, Edit]\n---\nYou design APIs.\nUse $1 wisely.\n")

	rel, out := codexAgentTransform("api-architect.md", source)
	want := "name = \"api-architect\"\ndescription = \"Designs \\\"good\\\" APIs\"\nmodel = \"gpt-5\"\ndeveloper_instructions = \"\"\"\nYou design APIs.\nUse $1 wisely.\"\"\"\n"
	if rel != "api-architect.toml" || string(out) != want {
		t.Fatalf("codex = %s\n%s", rel, out)
	}
	// The stem names an agent without a name field, and nested paths keep
	// their directory.
	rel, out = codexAgentTransform("team/helper.agent.md", []byte("Just a body\n"))
	if rel != "team/helper.toml" || !strings.HasPrefix(string(out), "name = \"helper\"\ndescription = \"\"\n") {
		t.Fatalf("codex nameless = %s\n%s", rel, out)
	}

	rel, out = copilotAgentTransform("api-architect.md", source)
	if rel != "api-architect.agent.md" || !bytes.Equal(out, source) {
		t.Fatalf("copilot = %s", rel)
	}
	if rel, _ := copilotAgentTransform("x.agent.md", source); rel != "x.agent.md" {
		t.Fatalf("copilot already named = %s", rel)
	}

	rel, out = geminiCommandTransform("review.md", []byte("---\ndescription: Review a PR\n---\nReview $ARGUMENTS carefully.\n"))
	if rel != "review.toml" || string(out) != "description = \"Review a PR\"\nprompt = \"Review {{args}} carefully.\"\n" {
		t.Fatalf("gemini = %s\n%s", rel, out)
	}
	_, out = geminiCommandTransform("deploy.md", []byte("Deploy $1 to $2\n"))
	if string(out) != "prompt = \"\"\"\nArguments: {{args}}\n\nDeploy $1 to $2\"\"\"\n" {
		t.Fatalf("gemini positional =\n%s", out)
	}

	// Non-markdown files pass through every transform untouched.
	for _, transform := range []dirTransform{codexAgentTransform, copilotAgentTransform, geminiCommandTransform} {
		if rel, out := transform("README.txt", []byte("x")); rel != "README.txt" || string(out) != "x" {
			t.Fatalf("passthrough = %s %q", rel, out)
		}
	}
}

// End to end: a directory source, a single-file git source, and a local
// file land under their basenames in each harness's directory, converted
// where the harness wants its own format.
func TestAgentsBlockMaterializesSubagentsAndCommands(t *testing.T) {
	root := t.TempDir()
	t.Setenv("JIG_CACHE_DIR", filepath.Join(root, "cache"))
	meta := filepath.Join(root, "meta")
	testFileSource(t, meta, map[string]string{
		"agents/reviewer.md":  "---\nname: reviewer\ndescription: Reviews code\n---\nReview it.\n",
		"agents/planner.md":   "---\ndescription: Plans work\n---\nPlan it.\n",
		"extra/architect.md":  "---\nname: architect\ndescription: Designs\n---\nDesign it.\n",
		"commands/review.md":  "---\ndescription: Review a PR\n---\nReview $ARGUMENTS.\n",
		"commands/README.txt": "not a command\n",
	})
	local := filepath.Join(root, "home", "deploy.md")
	if err := os.MkdirAll(filepath.Dir(local), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(local, []byte("Deploy $1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeTestWorkspace(t, root, fmt.Sprintf(`{
  "version": 4,
  "agents": {
    "subagents": [%q, %q],
    "commands": [%q, {"file": %q}]
  },
  "tree": {}
}`, meta+"#agents", meta+"#extra/architect.md", meta+"#commands", local))
	harnesses := []string{"claude", "codex", "copilot", "gemini"}
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
	if read(".claude/agents/reviewer.md") == "" || read(".claude/agents/planner.md") == "" || !strings.Contains(read(".claude/agents/architect.md"), "Design it.") {
		t.Fatal("claude agents")
	}
	if got := read(".codex/agents/architect.toml"); !strings.Contains(got, "name = \"architect\"") || !strings.Contains(got, "developer_instructions = \"Design it.\"") {
		t.Fatalf("codex architect =\n%s", got)
	}
	if got := read(".codex/agents/planner.toml"); !strings.HasPrefix(got, "name = \"planner\"\n") {
		t.Fatalf("codex planner =\n%s", got)
	}
	if read(".github/agents/reviewer.agent.md") == "" {
		t.Fatal("copilot agent")
	}
	for _, rel := range []string{".claude/commands/review.md", ".claude/commands/deploy.md", ".claude/commands/README.txt", ".gemini/commands/README.txt"} {
		read(rel)
	}
	if got := read(".gemini/commands/review.toml"); got != "description = \"Review a PR\"\nprompt = \"Review {{args}}.\"\n" {
		t.Fatalf("gemini review =\n%s", got)
	}
	if got := read(".gemini/commands/deploy.toml"); !strings.Contains(got, "Arguments: {{args}}") {
		t.Fatalf("gemini deploy =\n%s", got)
	}
	for _, rel := range []string{".agents", ".codex/commands", ".github/commands", ".gemini/agents", ".copilot"} {
		if pathExists(filepath.Join(root, filepath.FromSlash(rel))) {
			t.Fatalf("%s derived although nothing reads it", rel)
		}
	}

	// A second run is clean: the manifest records the transformed paths.
	out.Reset()
	if err := Sync(SyncOptions{SkipUpdate: true}, &out); err != nil {
		t.Fatalf("sync: %v\n%s", err, out.String())
	}
	if strings.Count(out.String(), "present-dir:") != 5 {
		t.Fatalf("second run:\n%s", out.String())
	}

	// A single-file source that is updated upstream flows through.
	if err := os.WriteFile(filepath.Join(meta, "extra", "architect.md"), []byte("---\nname: architect\n---\nDesign it better.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, meta, "commit", "-qam", "v2")
	out.Reset()
	if err := Sync(SyncOptions{SkipUpdate: true}, &out); err != nil {
		t.Fatalf("sync v2: %v\n%s", err, out.String())
	}
	if !strings.Contains(read(".codex/agents/architect.toml"), "Design it better.") {
		t.Fatalf("update did not flow:\n%s", out.String())
	}
}
