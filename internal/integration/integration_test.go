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

	// A local edit is never overwritten, even when upstream moves again;
	// the skipped entry makes the sync fail so scripts see it.
	w.writeFiles(w.path("ws", "scripts"), map[string]string{"dev.sh": "edited\n"})
	w.commitRemote(config, map[string]string{"scripts/dev.sh": "v3\n"}, "v3")
	out, err := w.jig(ws, "sync")
	if err == nil {
		t.Fatalf("expected sync to fail on a skipped entry:\n%s", out)
	}
	w.assertContains(out, "locally modified")
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
        { "src": "%s#skills", "onlyWhen": { "tags": ["frontend"] } }
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

	// Installing a frontend-tagged repo activates the tag-gated source;
	// removing it cleans the source's files up again.
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

// TestSafetyRefusals covers the "never lose work" promises end to end: rm
// refuses dirty and unpushed checkouts unless forced, sync refuses to move a
// dirty checkout, update refuses diverged schema history, and an invalid
// upstream schema is rejected before touching the checkout.
func TestSafetyRefusals(t *testing.T) {
	w := newWorld(t)
	app := w.newRemote("app", map[string]string{"README.md": "app\n"})
	schemaV1 := fmt.Sprintf(`{
  "version": 1,
  "tree": { "svc/app": { "$repo": { "id": "app", "git": "%s" } } }
}`, app)
	schemaRemote := w.newRemote("schema", map[string]string{"jig.json": schemaV1})
	w.mustJig(w.root, "init", schemaRemote, "ws", "--clone")
	ws := w.path("ws")
	appDir := w.path("ws", "svc", "app")

	// rm refuses a dirty checkout.
	w.writeFiles(appDir, map[string]string{"WIP.txt": "wip\n"})
	out, err := w.jig(ws, "rm", "svc/app")
	if err == nil {
		t.Fatalf("expected rm to refuse a dirty checkout:\n%s", out)
	}
	w.assertContains(out, "uncommitted changes")

	// rm refuses unpushed commits once the tree is clean but ahead.
	w.git(appDir, "add", "-A")
	w.git(appDir, "commit", "-qm", "wip")
	out, err = w.jig(ws, "rm", "svc/app")
	if err == nil {
		t.Fatalf("expected rm to refuse unpushed commits:\n%s", out)
	}
	w.assertContains(out, "unpushed")

	// sync refuses to move a checkout with uncommitted changes.
	w.writeFiles(appDir, map[string]string{"WIP2.txt": "wip\n"})
	schemaMoved := fmt.Sprintf(`{
  "version": 1,
  "tree": { "svc/renamed": { "$repo": { "id": "app", "git": "%s" } } }
}`, app)
	w.commitRemote(schemaRemote, map[string]string{"jig.json": schemaMoved}, "rename")
	out, err = w.jig(ws, "update", "--sync")
	if err == nil {
		t.Fatalf("expected update --sync to fail on the skipped move:\n%s", out)
	}
	w.assertContains(out, "uncommitted changes")
	if !w.exists("ws", "svc", "app", "WIP2.txt") || w.exists("ws", "svc", "renamed") {
		t.Fatal("expected dirty checkout left in place")
	}
	// Once clean, the same sync performs the move.
	w.git(appDir, "add", "-A")
	w.git(appDir, "commit", "-qm", "wip2")
	w.assertContains(w.mustJig(ws, "sync"), "moved: svc/renamed")
	if !w.exists("ws", "svc", "renamed", "WIP2.txt") {
		t.Fatal("expected checkout moved with local commits intact")
	}

	// An invalid upstream schema is rejected before touching the checkout.
	w.commitRemote(schemaRemote, map[string]string{"jig.json": `{"version": 1}`}, "broken")
	out, err = w.jig(ws, "update")
	if err == nil {
		t.Fatalf("expected update to reject an invalid upstream schema:\n%s", out)
	}
	// The live schema is untouched: the workspace still resolves.
	w.assertContains(w.mustJig(ws, "status"), "svc/renamed")

	// Diverged schema history is refused with a pointer to git.
	w.commitRemote(schemaRemote, map[string]string{"jig.json": schemaMoved}, "fixed")
	w.writeFiles(w.path("ws", ".jig", "source"), map[string]string{"local.txt": "local\n"})
	w.git(w.path("ws", ".jig", "source"), "add", "-A")
	w.git(w.path("ws", ".jig", "source"), "commit", "-qm", "local schema commit")
	out, err = w.jig(ws, "update")
	if err == nil {
		t.Fatalf("expected update to refuse diverged schema history:\n%s", out)
	}
	w.assertContains(out+err.Error(), "diverged")

	// --force overrides the rm safety checks.
	w.mustJig(ws, "rm", "-f", "svc/renamed")
	if w.exists("ws", "svc") {
		t.Fatal("expected forced rm to remove the checkout")
	}
}

// TestConditionsScopeAndArchived covers the planning solver's activation
// rules through the CLI: entry-level onlyWhen pulling repos and files in and
// out, scope-based file activation, updated-origin, and the archived
// lifecycle.
func TestConditionsScopeAndArchived(t *testing.T) {
	w := newWorld(t)
	api := w.newRemote("api", map[string]string{"README.md": "api\n"})
	debug := w.newRemote("debug", map[string]string{"README.md": "debug\n"})
	old := w.newRemote("old", map[string]string{"README.md": "old\n"})
	config := w.newRemote("config", map[string]string{"api.md": "api-notes\n"})
	schema := func(apiURL string) string {
		return fmt.Sprintf(`{
  "version": 1,
  "tree": {
    "services/api": { "$repo": { "id": "api", "git": "%s" } },
    "tools/debug": {
      "$repo": { "id": "debug", "git": "%s",
        "onlyWhen": { "path": "services" } }
    },
    "services/NOTES.md": { "$file": { "id": "notes", "src": "%s#api.md" } },
    "legacy/old": { "$repo": { "id": "old", "git": "%s", "archived": true } }
  }
}`, apiURL, debug, config, old)
	}
	schemaRemote := w.newRemote("schema", map[string]string{"jig.json": schema(api)})

	// Cloning the conditional repo alone is inert: its condition does not
	// hold, and neither scope files nor archived entries materialize.
	w.mustJig(w.root, "init", schemaRemote, "ws")
	ws := w.path("ws")
	w.mustJig(ws, "clone", "tools/debug", "--no-deps")
	if w.exists("ws", "services") || w.exists("ws", "legacy") {
		t.Fatal("expected nothing outside the selection")
	}

	// Installing services/api activates the conditional repo and the
	// scope-local file on the next sync.
	// Cloning services/api activates the conditional repo and the
	// scope-local file as part of the same plan.
	w.mustJig(ws, "clone", "services/api")
	w.mustJig(ws, "sync")
	if w.read("ws", "services", "NOTES.md") != "api-notes\n" {
		t.Fatal("expected scope-local file materialized")
	}
	if !w.exists("ws", "tools", "debug", ".git") {
		t.Fatal("expected conditional repo activated by installed services")
	}
	if w.exists("ws", "legacy") {
		t.Fatal("archived entry must stay uninstalled by default")
	}

	// Archived entries install only with --archived, then show the note and
	// keep syncing.
	w.assertContains(w.mustJig(ws, "clone", "legacy/old", "--archived"), "cloned: legacy/old")
	w.assertContains(w.mustJig(ws, "status"), "archived")
	w.assertContains(w.mustJig(ws, "sync"), "present: legacy/old")

	// A changed git URL in the schema updates origin on sync.
	apiMoved := w.newRemote("api-moved", map[string]string{"README.md": "api\n"})
	w.commitRemote(schemaRemote, map[string]string{"jig.json": schema(apiMoved)}, "move api hosting")
	w.assertContains(w.mustJig(ws, "update", "--sync"), "updated-origin: services/api")
	origin := strings.TrimSpace(w.git(w.path("ws", "services", "api"), "remote", "get-url", "origin"))
	if origin != apiMoved {
		t.Fatalf("origin = %q, want %q", origin, apiMoved)
	}

	// Removing the activating repo deactivates the conditional repo for
	// future plans (it stays installed but is no longer restored).
	w.mustJig(ws, "rm", "services/api", "tools/debug")
	w.assertNotContains(w.mustJig(ws, "sync"), "restored: tools/debug")
	if w.exists("ws", "tools") {
		t.Fatal("expected conditional repo to stay uninstalled")
	}
}

// TestSubdirScoping covers position-relative behavior: pathless commands
// scope to the subtree the command runs in, explicit paths resolve like
// filesystem paths (".", "..", leading "/" for the root), and running
// inside a checkout addresses that one repository.
func TestSubdirScoping(t *testing.T) {
	w := newWorld(t)
	app1 := w.newRemote("app1", map[string]string{"README.md": "app1\n"})
	app2 := w.newRemote("app2", map[string]string{"README.md": "app2\n"})
	schemaRemote := w.newRemote("schema", map[string]string{"jig.json": fmt.Sprintf(`{
  "version": 1,
  "tree": {
    "groupa/app1": { "$repo": { "id": "app1", "git": "%s" } },
    "groupb/app2": { "$repo": { "id": "app2", "git": "%s" } }
  }
}`, app1, app2)})
	w.mustJig(w.root, "init", schemaRemote, "ws", "--clone")
	ws := w.path("ws")
	inA := w.path("ws", "groupa")

	// Pathless status scopes to the subtree; ".." climbs; "/" reaches root.
	out := w.mustJig(inA, "status")
	w.assertContains(out, "groupa/app1")
	w.assertNotContains(out, "groupb/app2")
	w.assertContains(w.mustJig(inA, "status", ".."), "groupb/app2")
	w.assertContains(w.mustJig(inA, "info", "/groupb/app2"), "identity: app2")
	if _, err := w.jig(inA, "status", "../.."); err == nil {
		t.Fatal("expected error for a path outside the workspace")
	}

	// Scoped pull touches only the subtree.
	w.commitRemote(app1, map[string]string{"README.md": "v2\n"}, "v2")
	w.commitRemote(app2, map[string]string{"README.md": "v2\n"}, "v2")
	out = w.mustJig(inA, "pull")
	w.assertContains(out, "pulled: groupa/app1")
	w.assertNotContains(out, "groupb/app2")

	// Inside a checkout, pathless commands address that one repository.
	inApp2 := w.path("ws", "groupb", "app2")
	w.mustJig(inApp2, "fetch")
	out = w.mustJig(inApp2, "status")
	w.assertContains(out, "groupb/app2", "behind 1")
	w.assertNotContains(out, "groupa/app1")
	w.mustJig(inApp2, "pull")

	// Pathless sync converges only the subtree: a checkout deleted outside
	// the scope is not restored.
	if err := os.RemoveAll(w.path("ws", "groupb", "app2")); err != nil {
		t.Fatal(err)
	}
	out = w.mustJig(inA, "sync")
	w.assertNotContains(out, "groupb/app2")
	if w.exists("ws", "groupb", "app2") {
		t.Fatal("expected out-of-scope repo untouched by scoped sync")
	}
	w.assertContains(w.mustJig(ws, "sync"), "restored: groupb/app2")

	// --prune requires the workspace root.
	if _, err := w.jig(inA, "sync", "--prune"); err == nil {
		t.Fatal("expected --prune to require the workspace root")
	}

	// Relative rm works from the subtree (and prunes the emptied parent).
	w.mustJig(inA, "rm", "app1")
	if w.exists("ws", "groupa") {
		t.Fatal("expected relative rm to remove the checkout")
	}

	// Relative clone from the subtree reinstalls.
	w.assertContains(w.mustJig(w.path("ws", "groupb"), "clone", "/groupa/app1"), "cloned: groupa/app1")
}
