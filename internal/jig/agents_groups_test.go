package jig

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A group's agents block is the top-level block scoped to the group: its
// sources, servers, and hooks follow the group's repositories, a group's
// server overrides the workspace's of the same name while the group is
// active, and a context section from it is framed with the group.
func TestGroupAgentsBlocks(t *testing.T) {
	root := t.TempDir()
	t.Setenv("JIG_CACHE_DIR", filepath.Join(root, "cache"))
	remoteA := filepath.Join(root, "remote-a")
	remoteB := filepath.Join(root, "remote-b")
	testRemoteRepo(t, remoteA)
	testRemoteRepo(t, remoteB)
	meta := filepath.Join(root, "meta")
	testFileSource(t, meta, map[string]string{
		"context/workspace.md": "# Workspace\n",
		"context/payments.md":  "# Payments\nUse the billing API.\n",
	})
	schema := fmt.Sprintf(`{"version": 4,
	  "agents": {"context": [%q], "mcp": {"github": {"command": "npx"}}},
	  "tree": {
	    "services/": {
	      "platform/": {"auth": {"$repo": {"id": "auth", "git": %q}}},
	      "payments/": {
	        "$group": {"agents": {
	          "context": [%q],
	          "mcp": {"billing": {"url": "https://mcp.example.com/sse"}, "github": {"command": "gh-payments"}},
	          "hooks": [{"event": "stop", "command": "echo pay"}]
	        }},
	        "checkout": {"$repo": {"id": "checkout", "git": %q}}
	      }
	    }
	  }}`, meta+"#context/workspace.md", remoteA, meta+"#context/payments.md", remoteB)
	def := testDefinition(t, schema)
	if errs := validateDefinition(def).Errors; len(errs) != 0 {
		t.Fatalf("errors = %v", errs)
	}
	model, err := flattenDefinition(def, []string{"claude", "codex"})
	if err != nil {
		t.Fatal(err)
	}
	context := model.Entries["AGENTS.md"].File.Src
	if len(context) != 2 || context[0].scope != "" || context[1].scope != "services/payments" {
		t.Fatalf("context sources = %+v", context)
	}
	servers := model.Entries[".mcp.json"].File.managed.servers
	if servers["github"].Command != "gh-payments" || servers["github"].fallback == nil || servers["github"].fallback.Command != "npx" || servers["billing"].scope != "services/payments" {
		t.Fatalf("servers = %+v", servers)
	}

	// A group with no repository beneath it cannot carry a block, and a
	// block needs version 4.
	errs := validateDefinition(testDefinition(t, `{"version": 4, "tree": {"empty/": {"$group": {"agents": {"hooks": [{"event": "stop", "command": "x"}]}}}}}`)).Errors
	if len(errs) != 1 || !strings.Contains(errs[0], "group empty agents block") || !strings.Contains(errs[0], "does not match any repository") {
		t.Fatalf("empty group errors = %v", errs)
	}
	if _, err := flattenDefinition(testDefinition(t, `{"version": 3, "tree": {"g/": {"$group": {"agents": {"hooks": [{"event": "stop", "command": "x"}]}}, "a": {"$repo": {"git": "git@example.com:a.git"}}}}}`), nil); err == nil || !strings.Contains(err.Error(), "requires version 4") {
		t.Fatalf("version 3 = %v", err)
	}

	// With only the platform service installed, the payments block is
	// off: no payments section, the workspace's github server, no hook.
	writeTestWorkspace(t, root, schema)
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
	if err := Clone(CloneOptions{Selector: Selector{Path: "services/platform"}}, &out); err != nil {
		t.Fatalf("clone platform: %v\n%s", err, out.String())
	}
	if got := read("AGENTS.md"); !strings.Contains(got, "# Workspace") || strings.Contains(got, "Payments") {
		t.Fatalf("AGENTS.md with platform only =\n%s", got)
	}
	if got := read(".mcp.json"); !strings.Contains(got, "\"npx\"") || strings.Contains(got, "billing") {
		t.Fatalf(".mcp.json with platform only =\n%s", got)
	}
	if pathExists(filepath.Join(root, ".claude", "settings.json")) {
		t.Fatalf("hook document written with the group off:\n%s", out.String())
	}
	var status bytes.Buffer
	if err := Status(StatusOptions{}, &status); err != nil || strings.Contains(status.String(), "missing") {
		t.Fatalf("status: %v\n%s", err, status.String())
	}

	// Installing the payments service turns the block on.
	out.Reset()
	if err := Clone(CloneOptions{Selector: Selector{Path: "services/payments"}}, &out); err != nil {
		t.Fatalf("clone payments: %v\n%s", err, out.String())
	}
	if got := read("AGENTS.md"); !strings.Contains(got, "# Workspace\n\n## services/payments\n\nApplies to the repositories under `services/payments`.\n\n# Payments\n") {
		t.Fatalf("AGENTS.md with payments =\n%s", got)
	}
	if got := read(".mcp.json"); !strings.Contains(got, "\"gh-payments\"") || strings.Contains(got, "\"npx\"") || !strings.Contains(got, "\"billing\"") {
		t.Fatalf(".mcp.json with payments =\n%s", got)
	}
	if got := read(".claude/settings.json"); !strings.Contains(got, "echo pay") {
		t.Fatalf(".claude/settings.json =\n%s", got)
	}
	if got := read("CLAUDE.md"); !strings.Contains(got, "## services/payments") {
		t.Fatalf("CLAUDE.md =\n%s", got)
	}
}
