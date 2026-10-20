package jig

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepositoryExportsExpandAndValidate(t *testing.T) {
	schema := `{"version": 4, "tree": {
	  "tools": {
	    "$group": {"agents": {"skills": ".agents/skills"}},
	    "a": {"$repo": {"id": "a", "git": "git@example.com:a.git"}},
	    "b": {"$repo": {"id": "b", "git": "git@example.com:b.git", "agents": {"skills": "skills", "context": "AGENTS.md"}}}
	  },
	  "compose": {"$dir": {"src": [{"repo": {"id": "a"}, "path": "compose"}]}}
	}}`
	def := testDefinition(t, schema)
	if errs := validateDefinition(def).Errors; len(errs) != 0 {
		t.Fatalf("errors = %v", errs)
	}
	model, err := flattenDefinition(def, []string{"claude"})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(sortedEntryPaths(&model), " "); got != ".claude/skills CLAUDE.md compose tools tools/a tools/b" {
		t.Fatalf("entries = %s", got)
	}
	var skills []string
	for _, source := range model.Entries[".claude/skills"].Dir.Src {
		skills = append(skills, source.describe())
	}
	if got := strings.Join(skills, " "); got != "repo:a#.agents/skills repo:b#skills" {
		t.Fatalf("skills sources = %s", got)
	}
	if got := model.Entries["CLAUDE.md"].File.Src[0].describe(); got != "repo:b#AGENTS.md" {
		t.Fatalf("context source = %s", got)
	}

	if _, err := flattenDefinition(testDefinition(t, strings.Replace(schema, `"version": 4`, `"version": 3`, 1)), nil); err == nil || !strings.Contains(err.Error(), "require version 4") {
		t.Fatalf("version 3 = %v", err)
	}

	errs := validateDefinition(testDefinition(t, `{"version": 4, "tree": {
	  "tools": {
	    "$group": {"agents": {"rules": "rules"}},
	    "a": {"$repo": {"id": "a", "git": "git@example.com:a.git", "agents": {"skills": "../skills"}}},
	    "b": {"$repo": {"id": "b", "git": "git@example.com:b.git"}}
	  },
	  "x": {"$dir": {"src": [{"repo": {"id": "nope"}, "path": "p"}, {"repo": {"tags": ["t"]}, "path": "p"}, {"repo": {"id": "a"}}, {"repo": {"path": "x"}, "path": "p"}]}}
	}}`)).Errors
	joined := strings.Join(errs, "\n")
	for _, want := range []string{`agents export "rules" is not exportable`, `agents export skills has an invalid path "../skills"`, "repository nope does not name any entry", "must reference one repository by id or exact path", "needs a path inside the repository", "checkout source x names a dir, not a repository"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in:\n%s", want, joined)
		}
	}
	if strings.Count(joined, "not exportable") != 1 {
		t.Fatalf("group error repeated:\n%s", joined)
	}
}

// End to end: a repository's skills and context land at the root and in
// the harness copies while it is installed, follow edits in the checkout,
// a repository without the path contributes nothing, a $dir checkout
// source works the same way, and removing the repository takes it all
// out again.
func TestRepositoryExportsMaterialize(t *testing.T) {
	root := t.TempDir()
	t.Setenv("JIG_CACHE_DIR", filepath.Join(root, "cache"))
	remoteA := filepath.Join(root, "remotes", "a")
	remoteB := filepath.Join(root, "remotes", "b")
	testRemoteRepo(t, remoteA)
	testRemoteRepo(t, remoteB)
	testFileSource(t, remoteA, map[string]string{
		".agents/skills/alpha/SKILL.md": "---\nname: alpha\n---\nalpha v1\n",
		"AGENTS.md":                     "# Alpha\nUse alpha.\n",
		"compose/docker-compose.yml":    "services: {}\n",
	})
	writeTestWorkspace(t, root, fmt.Sprintf(`{"version": 4, "tree": {
	  "tools": {
	    "$group": {"agents": {"skills": ".agents/skills", "context": "AGENTS.md"}},
	    "a": {"$repo": {"id": "a", "git": %q}},
	    "b": {"$repo": {"id": "b", "git": %q}}
	  },
	  "compose": {"$dir": {"id": "compose", "src": [{"repo": {"id": "a"}, "path": "compose"}]}}
	}}`, remoteA, remoteB))
	harnesses := []string{"claude", "codex"}
	if err := saveConfig(root, Config{Version: 1, Schema: "jig.json", Harnesses: &harnesses}); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	read := func(rel string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("%s: %v", rel, err)
		}
		return string(data)
	}

	var out bytes.Buffer
	if err := Clone(CloneOptions{}, &out); err != nil {
		t.Fatalf("clone: %v\n%s", err, out.String())
	}
	if strings.Contains(out.String(), "unavailable") {
		t.Fatalf("a repository without the path must contribute silently:\n%s", out.String())
	}
	for _, rel := range []string{".agents/skills/alpha/SKILL.md", ".claude/skills/alpha/SKILL.md", "compose/docker-compose.yml"} {
		if !pathExists(filepath.Join(root, filepath.FromSlash(rel))) {
			t.Fatalf("missing %s:\n%s", rel, out.String())
		}
	}
	if got := read("AGENTS.md"); !strings.Contains(got, "Use alpha.") {
		t.Fatalf("AGENTS.md =\n%s", got)
	}
	state, err := loadState(root)
	if err != nil {
		t.Fatal(err)
	}
	if src := state.Dirs["agents-skills"].Src; src != "repo:a#.agents/skills" {
		t.Fatalf("state src = %q", src)
	}

	// An edit in the checkout, not yet committed, flows on the next sync.
	if err := os.WriteFile(filepath.Join(root, "tools", "a", ".agents", "skills", "alpha", "SKILL.md"), []byte("---\nname: alpha\n---\nalpha v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := Sync(SyncOptions{SkipUpdate: true}, &out); err != nil {
		t.Fatalf("sync: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "updated-dir: .agents/skills") || !strings.Contains(read(".claude/skills/alpha/SKILL.md"), "alpha v2") {
		t.Fatalf("edit did not flow:\n%s", out.String())
	}
	out.Reset()
	if err := Sync(SyncOptions{SkipUpdate: true}, &out); err != nil {
		t.Fatalf("sync: %v\n%s", err, out.String())
	}
	if strings.Contains(out.String(), "updated-dir") || strings.Contains(out.String(), "wrote-") {
		t.Fatalf("third sync not clean:\n%s", out.String())
	}

	// Removing the repository gates its exports off: the skills, the
	// context, and the compose directory go.
	out.Reset()
	if err := Remove(RemoveOptions{Paths: []string{"tools/a"}, Force: true}, &out); err != nil {
		t.Fatalf("rm: %v\n%s", err, out.String())
	}
	out.Reset()
	if err := Sync(SyncOptions{SkipUpdate: true}, &out); err != nil {
		t.Fatalf("sync after rm: %v\n%s", err, out.String())
	}
	for _, rel := range []string{".agents/skills/alpha", ".claude/skills/alpha", "compose/docker-compose.yml", "AGENTS.md"} {
		if pathEntryExists(filepath.Join(root, filepath.FromSlash(rel))) {
			t.Fatalf("%s survived the removal:\n%s", rel, out.String())
		}
	}
}
