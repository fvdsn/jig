package integration

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// TestWorkspaceLifecycle covers the core loop: init from a schema repo with
// dependency-aware cloning, quiet status, restore-on-sync, rm as the
// uninstall verb, and --no-deps.
func TestWorkspaceLifecycle(t *testing.T) {
	w := newWorld(t)
	auth := w.newRemote("auth", map[string]string{"README.md": "auth\n"})
	checkout := w.newRemote("checkout", map[string]string{"README.md": "checkout\n"})
	util := w.newRemote("util", map[string]string{"README.md": "util\n"})
	w.newRemote("schema", map[string]string{"jig.json": fmt.Sprintf(`{
  "version": 1,
  "tree": {
    "platform/auth": { "$repo": { "id": "auth", "git": "%s" } },
    "services/checkout": {
      "$repo": { "id": "checkout", "git": "%s",
        "dependsOn": [{ "path": "platform" }] }
    },
    "tools/util": { "$repo": { "id": "util", "git": "%s" } }
  }
}`, auth, checkout, util)})

	// init --clone brings the selection plus its dependencies, nothing else.
	out := w.mustJig(w.root, "init", w.path("remotes", "schema"), "ws", "--clone", "services/checkout")
	w.assertContains(out, "cloned: services/checkout", "cloned: platform/auth")
	ws := w.path("ws")
	if !w.exists("ws", "services", "checkout", "README.md") || !w.exists("ws", "platform", "auth", ".git") {
		t.Fatal("expected checkout and its dependency on disk")
	}
	if w.exists("ws", "tools") {
		t.Fatal("did not expect unselected repo cloned")
	}

	// deps reports the dependency closure.
	w.assertContains(w.mustJig(ws, "deps", "services/checkout"), "platform/auth")

	// Status reports the workspace, counting uninstalled catalog entries.
	out = w.mustJig(ws, "status")
	w.assertContains(out, "not installed")
	w.assertNotContains(out, "tools/util")
	w.assertContains(w.mustJig(ws, "status", "--all"), "tools/util")

	// Deleting a checkout by hand is not an uninstall: sync restores it.
	if err := os.RemoveAll(w.path("ws", "platform", "auth")); err != nil {
		t.Fatal(err)
	}
	w.assertContains(w.mustJig(ws, "sync"), "restored: platform/auth")
	if !w.exists("ws", "platform", "auth", "README.md") {
		t.Fatal("expected restored checkout")
	}

	// rm uninstalls; sync no longer restores; clone reinstalls.
	w.mustJig(ws, "rm", "services/checkout")
	if w.exists("ws", "services") {
		t.Fatal("expected checkout removed and parent pruned")
	}
	w.assertNotContains(w.mustJig(ws, "sync"), "restored:")
	if w.exists("ws", "services") {
		t.Fatal("expected sync to leave removed repo uninstalled")
	}

	// --no-deps clones just the selection.
	w.mustJig(ws, "rm", "platform/auth")
	w.assertContains(w.mustJig(ws, "clone", "services/checkout", "--no-deps"), "cloned: services/checkout")
	if w.exists("ws", "platform") {
		t.Fatal("did not expect dependency with --no-deps")
	}
}

// TestSchemaEvolution covers the shared-schema flow: a teammate renames an
// entry and removes another upstream; update --sync moves the checkout and
// reports the stale one; sync --prune deletes it under rm safety rules.
func TestSchemaEvolution(t *testing.T) {
	w := newWorld(t)
	svc := w.newRemote("svc", map[string]string{"README.md": "svc\n"})
	legacy := w.newRemote("legacy", map[string]string{"README.md": "legacy\n"})
	schemaV1 := fmt.Sprintf(`{
  "version": 1,
  "tree": {
    "services/checkout": { "$repo": { "id": "checkout", "git": "%s" } },
    "tools/legacy": { "$repo": { "id": "legacy", "git": "%s" } }
  }
}`, svc, legacy)
	schemaRemote := w.newRemote("schema", map[string]string{"jig.json": schemaV1})

	w.mustJig(w.root, "init", schemaRemote, "ws", "--clone")
	ws := w.path("ws")
	if !w.exists("ws", "services", "checkout", ".git") || !w.exists("ws", "tools", "legacy", ".git") {
		t.Fatal("expected both repos installed")
	}

	// Upstream: rename services/checkout to services/cart (same identity),
	// drop tools/legacy entirely.
	schemaV2 := fmt.Sprintf(`{
  "version": 1,
  "tree": {
    "services/cart": { "$repo": { "id": "checkout", "git": "%s" } }
  }
}`, svc)
	w.commitRemote(schemaRemote, map[string]string{"jig.json": schemaV2}, "rename and drop")

	out := w.mustJig(ws, "update", "--sync")
	w.assertContains(out, "moved: services/cart", "stale")
	if !w.exists("ws", "services", "cart", "README.md") || w.exists("ws", "services", "checkout") {
		t.Fatal("expected checkout moved to services/cart")
	}
	if !w.exists("ws", "tools", "legacy", ".git") {
		t.Fatal("expected stale repo kept without --prune")
	}

	// --prune removes what left the schema, with rm safety rules.
	w.mustJig(ws, "sync", "--prune")
	if w.exists("ws", "tools") {
		t.Fatal("expected stale repo pruned")
	}
	w.assertNotContains(w.mustJig(ws, "status"), "stale")
}

// TestFilesDirsAndLinks covers generated support artifacts end to end: a
// $file that updates when its source changes but never clobbers local edits,
// a multi-source $dir merge, and a harness symlink via dir link.
func TestFilesDirsAndLinks(t *testing.T) {
	w := newWorld(t)
	app := w.newRemote("app", map[string]string{"README.md": "app\n"})
	config := w.newRemote("config", map[string]string{"scripts/dev.sh": "v1\n"})
	ezSkills := w.newRemote("ez-skills", map[string]string{"skills/A/SKILL.md": "A\n"})
	moreSkills := w.newRemote("more-skills", map[string]string{"skills/B/SKILL.md": "B\n"})
	schemaRemote := w.newRemote("schema", map[string]string{"jig.json": fmt.Sprintf(`{
  "version": 1,
  "tree": {
    "app": { "$repo": { "id": "app", "git": "%s" } },
    "scripts/dev.sh": { "$file": { "id": "dev", "src": "%s#scripts/dev.sh" } },
    ".agents/skills": {
      "$dir": { "id": "skills", "src": ["%s#skills", "%s#skills"] }
    },
    ".claude/skills": { "$dir": { "id": "claude-skills", "link": ".agents/skills" } }
  }
}`, app, config, ezSkills, moreSkills)})

	w.mustJig(w.root, "init", schemaRemote, "ws", "--clone")
	ws := w.path("ws")
	if w.read("ws", "scripts", "dev.sh") != "v1\n" {
		t.Fatal("expected dev.sh v1")
	}
	if !w.exists("ws", ".agents", "skills", "A", "SKILL.md") || !w.exists("ws", ".agents", "skills", "B", "SKILL.md") {
		t.Fatal("expected merged skills from both sources")
	}
	if !w.exists("ws", ".claude", "skills", "A", "SKILL.md") {
		t.Fatal("expected skills reachable through the harness link")
	}
	if target, err := os.Readlink(w.path("ws", ".claude", "skills")); err != nil || target != "../.agents/skills" {
		t.Fatalf("harness link = %q, %v", target, err)
	}

	// An upstream change flows through sync.
	w.commitRemote(config, map[string]string{"scripts/dev.sh": "v2\n"}, "v2")
	w.assertContains(w.mustJig(ws, "sync"), "updated-file: scripts/dev.sh")
	if w.read("ws", "scripts", "dev.sh") != "v2\n" {
		t.Fatal("expected dev.sh v2")
	}

	// A local edit is never overwritten, even when upstream moves again.
	w.writeFiles(w.path("ws", "scripts"), map[string]string{"dev.sh": "edited\n"})
	w.commitRemote(config, map[string]string{"scripts/dev.sh": "v3\n"}, "v3")
	w.assertContains(w.mustJig(ws, "sync"), "locally modified")
	if w.read("ws", "scripts", "dev.sh") != "edited\n" {
		t.Fatal("expected local edit preserved")
	}

	// Removing the link removes only the symlink.
	w.mustJig(ws, "rm", ".claude/skills")
	if w.exists("ws", ".claude") {
		t.Fatal("expected harness link removed")
	}
	if !w.exists("ws", ".agents", "skills", "A", "SKILL.md") {
		t.Fatal("expected skills untouched after link removal")
	}
}

// TestFetchStatusPullCheckout covers the daily loop against a moving remote:
// fetch surfaces behind counts in status, pull fast-forwards, and checkout
// switches branches across the workspace.
func TestFetchStatusPullCheckout(t *testing.T) {
	w := newWorld(t)
	app := w.newRemote("app", map[string]string{"README.md": "v1\n"})
	schemaRemote := w.newRemote("schema", map[string]string{"jig.json": fmt.Sprintf(`{
  "version": 1,
  "tree": { "app": { "$repo": { "id": "app", "git": "%s" } } }
}`, app)})

	w.mustJig(w.root, "init", schemaRemote, "ws", "--clone")
	ws := w.path("ws")

	w.commitRemote(app, map[string]string{"README.md": "v2\n"}, "v2")
	w.mustJig(ws, "fetch")
	w.assertContains(w.mustJig(ws, "status"), "behind 1")
	w.assertContains(w.mustJig(ws, "pull"), "pulled: app")
	w.assertNotContains(w.mustJig(ws, "status"), "behind")
	if w.read("ws", "app", "README.md") != "v2\n" {
		t.Fatal("expected pulled content")
	}

	w.assertContains(w.mustJig(ws, "checkout", "-b", "feature"), "created: app")
	if branch := strings.TrimSpace(w.git(w.path("ws", "app"), "branch", "--show-current")); branch != "feature" {
		t.Fatalf("branch = %q, want feature", branch)
	}
	// Checkout is a plain (non-forced) git checkout: uncommitted changes are
	// carried to the new branch, never discarded.
	w.writeFiles(w.path("ws", "app"), map[string]string{"README.md": "dirty\n"})
	w.assertContains(w.mustJig(ws, "checkout", "-b", "other"), "created: app")
	if w.read("ws", "app", "README.md") != "dirty\n" {
		t.Fatal("expected dirty change carried across checkout")
	}
}

// TestTagsSelectAndGateSources covers tag-filtered cloning and per-source
// onlyWhen gating of a merged skills dir reacting to installs and removals.
func TestTagsSelectAndGateSources(t *testing.T) {
	w := newWorld(t)
	api := w.newRemote("api", map[string]string{"README.md": "api\n"})
	web := w.newRemote("web", map[string]string{"README.md": "web\n"})
	baseSkills := w.newRemote("base-skills", map[string]string{"skills/base/SKILL.md": "base\n"})
	webSkills := w.newRemote("web-skills", map[string]string{"skills/web/SKILL.md": "web\n"})
	schemaRemote := w.newRemote("schema", map[string]string{"jig.json": fmt.Sprintf(`{
  "version": 1,
  "tree": {
    "services/api": { "$repo": { "id": "api", "git": "%s", "tags": ["backend"] } },
    "services/web": { "$repo": { "id": "web", "git": "%s", "tags": ["frontend"] } },
    ".agents/skills": {
      "$dir": { "id": "skills", "src": [
        "%s#skills",
        { "src": "%s#skills", "onlyWhen": { "path": "services/web" } }
      ] }
    }
  }
}`, api, web, baseSkills, webSkills)})

	w.mustJig(w.root, "init", schemaRemote, "ws", "--clone", "--tags", "backend")
	ws := w.path("ws")
	if !w.exists("ws", "services", "api", ".git") || w.exists("ws", "services", "web") {
		t.Fatal("expected only backend-tagged repo cloned")
	}
	if !w.exists("ws", ".agents", "skills", "base", "SKILL.md") {
		t.Fatal("expected ungated skills source materialized")
	}
	if w.exists("ws", ".agents", "skills", "web") {
		t.Fatal("did not expect gated skills without services/web")
	}

	// Installing the gating repo activates the gated source; removing it
	// cleans the source's files up again.
	w.mustJig(ws, "clone", "services/web")
	w.mustJig(ws, "sync")
	if !w.exists("ws", ".agents", "skills", "web", "SKILL.md") {
		t.Fatal("expected gated skills after installing services/web")
	}
	w.mustJig(ws, "rm", "services/web")
	w.mustJig(ws, "sync")
	if w.exists("ws", ".agents", "skills", "web") {
		t.Fatal("expected gated skills removed after uninstalling services/web")
	}
	if !w.exists("ws", ".agents", "skills", "base", "SKILL.md") {
		t.Fatal("expected ungated skills untouched")
	}

	w.assertContains(w.mustJig(ws, "list", "--tags", "backend"), "services/api")
	w.assertNotContains(w.mustJig(ws, "list", "--tags", "backend"), "services/web")
}
