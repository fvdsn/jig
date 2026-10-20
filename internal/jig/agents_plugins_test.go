package jig

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseMarketplace(t *testing.T) {
	plugins, skipped, err := parseMarketplace([]byte(`{"plugins": [
	  {"name": "beta", "source": "./delivery/beta"},
	  {"name": "alpha", "source": "alpha"},
	  {"name": "remote", "source": {"source": "github", "repo": "org/x"}},
	  {"name": "up", "source": "../elsewhere"},
	  {"name": "abs", "source": "/abs"},
	  {"name": "url", "source": "https://example.com/x"}
	]}`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(plugins, ",") != "alpha,delivery/beta" || strings.Join(skipped, ",") != "remote,up,abs,url" {
		t.Fatalf("plugins = %v skipped = %v", plugins, skipped)
	}
	if _, _, err := parseMarketplace([]byte("nope")); err == nil {
		t.Fatal("invalid json accepted")
	}
}

func TestRewriteLinks(t *testing.T) {
	rw := &pluginRewrite{
		storePath: ".agents/plugins/lib/alpha",
		files:     map[string]bool{"references/rules.md": true, "skills/review/SKILL.md": true, "skills/review/notes.md": true, "agents/reviewer.md": true, "scripts/run.sh": true},
		subdir:    "skills",
	}
	in := "See [rules](../../references/rules.md#intro), [notes](notes.md), [run](../../scripts/run.sh?x=1), [gone](../../references/missing.md), [out](../../../outside.md), [web](https://x.y/z), [abs](/root.md), [frag](#here), [self](SKILL.md).\n"
	got := string(rw.rewriteLinks("review/SKILL.md", []byte(in), ".claude/skills/review"))
	want := "See [rules](../../../.agents/plugins/lib/alpha/references/rules.md#intro), [notes](notes.md), [run](../../../.agents/plugins/lib/alpha/scripts/run.sh?x=1), [gone](../../references/missing.md), [out](../../../outside.md), [web](https://x.y/z), [abs](/root.md), [frag](#here), [self](SKILL.md).\n"
	if got != want {
		t.Fatalf("skill rewrite =\n%s\nwant\n%s", got, want)
	}
	// From the canonical skills dir the path is shorter, and an agent's
	// unit is the file itself, so a link to a sibling agent is rewritten.
	got = string(rw.rewriteLinks("review/SKILL.md", []byte("[r](../../references/rules.md)"), ".agents/skills/review"))
	if got != "[r](../../plugins/lib/alpha/references/rules.md)" {
		t.Fatalf("canonical rewrite = %s", got)
	}
	rw.subdir = "agents"
	got = string(rw.rewriteLinks("reviewer.md", []byte("[r](../references/rules.md) [s](reviewer.md) [n](../skills/review/notes.md)"), ".codex/agents"))
	if got != "[r](../../.agents/plugins/lib/alpha/references/rules.md) [s](reviewer.md) [n](../../.agents/plugins/lib/alpha/skills/review/notes.md)" {
		t.Fatalf("agent rewrite = %s", got)
	}
	if out := rw.rewriteLinks("notes.txt", []byte("[r](../references/rules.md)"), ".codex/agents"); string(out) != "[r](../references/rules.md)" {
		t.Fatal("non-markdown rewritten")
	}
}

// testMarketplace builds a marketplace repository with two plugins: alpha
// has skills, an agent, and shared references they link to; beta has a
// command. The marketplace also lists a remote plugin, which is skipped.
func testMarketplace(t *testing.T, dir string) {
	t.Helper()
	testFileSource(t, dir, map[string]string{
		".claude-plugin/marketplace.json":   `{"name": "lib", "plugins": [{"name": "alpha", "source": "./alpha"}, {"name": "beta", "source": "./delivery/beta"}, {"name": "remote", "source": {"source": "github", "repo": "org/x"}}]}`,
		"alpha/.claude-plugin/plugin.json":  `{"name": "alpha", "version": "1.0.0"}`,
		"alpha/skills/pdlc-review/SKILL.md": "---\nname: pdlc-review\n---\nFollow [the rules](../../references/rules.md) and [notes](notes.md).\n",
		"alpha/skills/pdlc-review/notes.md": "notes\n",
		"alpha/skills/shared/SKILL.md":      "plugin version of shared\n",
		"alpha/agents/reviewer.md":          "---\nname: reviewer\ndescription: Reviews\n---\nUse [the rules](../references/rules.md#core).\n",
		"alpha/references/rules.md":         "# Rules\n",
		"alpha/README.md":                   "alpha readme\n",
		"delivery/beta/.plugin/plugin.json": `{"name": "beta"}`,
		"delivery/beta/commands/deploy.md":  "---\ndescription: Deploy\n---\nDeploy $ARGUMENTS\n",
	})
}

// The store, the per-harness directories fed by the plugins, the
// rewritten links, explicit sources winning, and the skipped remote
// entry, end to end.
func TestAgentsBlockMaterializesPlugins(t *testing.T) {
	root := t.TempDir()
	t.Setenv("JIG_CACHE_DIR", filepath.Join(root, "cache"))
	lib := filepath.Join(root, "pdlc-lib")
	testMarketplace(t, lib)
	explicit := testSkillsSource(t, root, "own-skills", map[string]string{"shared/SKILL.md": "explicit version of shared\n"})
	writeTestWorkspace(t, root, fmt.Sprintf(`{
  "version": 4,
  "agents": {
    "plugins": [%q],
    "skills": [%q]
  },
  "tree": {}
}`, lib, explicit+"#skills"))
	harnesses := []string{"claude", "codex"}
	if err := saveConfig(root, Config{Version: 1, Schema: "jig.json", Harnesses: &harnesses}); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	model, err := flattenDefinition(testDefinition(t, fmt.Sprintf(`{"version": 4, "agents": {"plugins": [%q]}, "tree": {}}`, lib)), []string{"claude"})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(sortedEntryPaths(&model), " "); got != ".agents/plugins .claude/agents .claude/commands .claude/skills" {
		t.Fatalf("entries = %s", got)
	}
	if store := model.Entries[".agents/plugins"]; store.Identity != "agents-plugins" || !store.Dir.store {
		t.Fatalf("store = %+v", store.Dir)
	}
	if errs := validateDefinition(testDefinition(t, fmt.Sprintf(`{"version": 4, "agents": {"plugins": [%q, {"file": "~/x"}]}, "tree": {}}`, lib))).Errors; len(errs) != 1 || !strings.Contains(errs[0], "dir .agents/plugins invalid src") {
		t.Fatalf("validation errors = %v", errs)
	}

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
	// The store holds the whole marketplace under the repository's name.
	read(".agents/plugins/pdlc-lib/alpha/references/rules.md")
	read(".agents/plugins/pdlc-lib/alpha/README.md")
	read(".agents/plugins/pdlc-lib/delivery/beta/commands/deploy.md")
	// Skills land in both skills directories with links into the store,
	// relative to each copy; links inside the skill stay.
	if got := read(".agents/skills/pdlc-review/SKILL.md"); !strings.Contains(got, "[the rules](../../plugins/pdlc-lib/alpha/references/rules.md)") || !strings.Contains(got, "[notes](notes.md)") {
		t.Fatalf(".agents/skills/pdlc-review =\n%s", got)
	}
	if got := read(".claude/skills/pdlc-review/SKILL.md"); !strings.Contains(got, "[the rules](../../../.agents/plugins/pdlc-lib/alpha/references/rules.md)") {
		t.Fatalf(".claude/skills/pdlc-review =\n%s", got)
	}
	read(".claude/skills/pdlc-review/notes.md")
	// The explicit source wins over the plugin's same-named skill.
	if got := read(".claude/skills/shared/SKILL.md"); got != "explicit version of shared\n" {
		t.Fatalf("shared = %q", got)
	}
	if !strings.Contains(out.String(), "shadowed") {
		t.Fatalf("expected a shadowed report:\n%s", out.String())
	}
	// Agents: Claude verbatim, Codex as TOML with the link rewritten first.
	if got := read(".claude/agents/reviewer.md"); !strings.Contains(got, "[the rules](../../.agents/plugins/pdlc-lib/alpha/references/rules.md#core)") {
		t.Fatalf(".claude/agents/reviewer =\n%s", got)
	}
	if got := read(".codex/agents/reviewer.toml"); !strings.Contains(got, "name = \"reviewer\"") || !strings.Contains(got, "../../.agents/plugins/pdlc-lib/alpha/references/rules.md#core") {
		t.Fatalf(".codex/agents/reviewer =\n%s", got)
	}
	read(".claude/commands/deploy.md")
	if !strings.Contains(out.String(), "skipped plugin remote:") {
		t.Fatalf("expected the remote entry to be reported:\n%s", out.String())
	}

	out.Reset()
	if err := Sync(SyncOptions{SkipUpdate: true}, &out); err != nil {
		t.Fatalf("sync: %v\n%s", err, out.String())
	}
	if strings.Contains(out.String(), "updated-dir") || strings.Contains(out.String(), "wrote-dir") {
		t.Fatalf("second run not clean:\n%s", out.String())
	}

	// A change in the plugin flows into the store and the skills.
	if err := os.WriteFile(filepath.Join(lib, "alpha", "skills", "pdlc-review", "SKILL.md"), []byte("v2 [the rules](../../references/rules.md)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, lib, "commit", "-qam", "v2")
	out.Reset()
	if err := Sync(SyncOptions{SkipUpdate: true}, &out); err != nil {
		t.Fatalf("sync v2: %v\n%s", err, out.String())
	}
	if !strings.HasPrefix(read(".claude/skills/pdlc-review/SKILL.md"), "v2 [the rules](../../../.agents/plugins/") || !strings.HasPrefix(read(".agents/plugins/pdlc-lib/alpha/skills/pdlc-review/SKILL.md"), "v2 ") {
		t.Fatalf("plugin update did not flow:\n%s", out.String())
	}
}

// A single plugin source lands under its own name in the store, and a
// local marketplace works the same way as a git one.
func TestAgentsBlockPluginSourceForms(t *testing.T) {
	root := t.TempDir()
	t.Setenv("JIG_CACHE_DIR", filepath.Join(root, "cache"))
	lib := filepath.Join(root, "pdlc-lib")
	testMarketplace(t, lib)
	local := filepath.Join(root, "home", "my-plugins")
	if err := os.MkdirAll(filepath.Join(local, ".plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	for rel, content := range map[string]string{
		".plugin/plugin.json":         `{"name": "mine"}`,
		"skills/local-skill/SKILL.md": "local [ref](../../references/r.md)\n",
		"references/r.md":             "r\n",
	} {
		p := filepath.Join(local, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeTestWorkspace(t, root, fmt.Sprintf(`{
  "version": 4,
  "agents": {"plugins": [%q, {"dir": %q}]},
  "tree": {}
}`, lib+"#alpha", local))
	none := []string{}
	if err := saveConfig(root, Config{Version: 1, Schema: "jig.json", Harnesses: &none}); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	var out bytes.Buffer
	if err := Clone(CloneOptions{}, &out); err != nil {
		t.Fatalf("clone: %v\n%s", err, out.String())
	}
	for _, rel := range []string{
		".agents/plugins/alpha/references/rules.md",
		".agents/plugins/my-plugins/references/r.md",
		".agents/skills/pdlc-review/SKILL.md",
		".agents/skills/local-skill/SKILL.md",
		".agents/agents/reviewer.md",
	} {
		if !pathExists(filepath.Join(root, filepath.FromSlash(rel))) {
			t.Fatalf("missing %s:\n%s", rel, out.String())
		}
	}
	data, _ := os.ReadFile(filepath.Join(root, ".agents", "skills", "local-skill", "SKILL.md"))
	if string(data) != "local [ref](../../plugins/my-plugins/references/r.md)\n" {
		t.Fatalf("local plugin skill = %q", data)
	}
	data, _ = os.ReadFile(filepath.Join(root, ".agents", "skills", "pdlc-review", "SKILL.md"))
	if !strings.Contains(string(data), "(../../plugins/alpha/references/rules.md)") {
		t.Fatalf("single plugin skill = %q", data)
	}
	if pathExists(filepath.Join(root, ".agents", "commands")) {
		t.Fatal("beta's command derived from a single-plugin source")
	}
}
