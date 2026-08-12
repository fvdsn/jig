package jig

import (
	"bytes"
	"os"
	"testing"
)

func TestGraphRendersMermaidFlowchart(t *testing.T) {
	root := t.TempDir()
	writeTestWorkspace(t, root, `{
  "version": 2,
  "tree": {
    "platform/auth": {
      "$repo": { "git": "git@example.com:auth.git" }
    },
    "platform/billing": {
      "$repo": {
        "git": "git@example.com:billing.git",
        "dependsOn": [{ "path": "platform/auth" }]
      }
    },
    "services/checkout": {
      "$repo": {
        "git": "git@example.com:checkout.git",
        "dependsOn": [{ "path": "platform/*" }]
      }
    },
    "tools/debug": {
      "$repo": {
        "git": "git@example.com:debug.git",
        "dependsOn": [{ "path": "platform/auth", "optional": true }]
      }
    }
  }
}`)

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(oldWd); err != nil {
			t.Fatal(err)
		}
	}()

	graph := func(options GraphOptions) string {
		t.Helper()
		var out bytes.Buffer
		if err := Graph(options, &out); err != nil {
			t.Fatalf("graph: %v\n%s", err, out.String())
		}
		return out.String()
	}

	// The whole schema: the tree renders as subgraphs, the group dependency
	// points at the platform subgraph, and the optional edge is dashed.
	got := graph(GraphOptions{})
	want := `flowchart TD
  subgraph platform ["platform"]
    platform_auth["auth"]
    platform_billing["billing"]
  end
  subgraph services ["services"]
    services_checkout["checkout"]
  end
  subgraph tools ["tools"]
    tools_debug["debug"]
  end
  platform_billing --> platform_auth
  services_checkout --> platform
  tools_debug -.-> platform_auth
`
	if got != want {
		t.Fatalf("graph = %q, want %q", got, want)
	}

	// A path scopes the selection; a group target with no drawn repos
	// becomes a plain node so the edge does not dangle.
	got = graph(GraphOptions{Path: "services"})
	want = `flowchart TD
  subgraph services ["services"]
    services_checkout["checkout"]
  end
  platform["platform"]
  services_checkout --> platform
`
	if got != want {
		t.Fatalf("graph services = %q, want %q", got, want)
	}

	// An edge target outside the selection is drawn as a context node in
	// its place in the tree.
	got = graph(GraphOptions{Path: "tools"})
	want = `flowchart TD
  subgraph platform ["platform"]
    platform_auth["auth"]
  end
  subgraph tools ["tools"]
    tools_debug["debug"]
  end
  tools_debug -.-> platform_auth
`
	if got != want {
		t.Fatalf("graph tools = %q, want %q", got, want)
	}
}

func TestMermaidIDEscapesKeywordsAndSymbols(t *testing.T) {
	if got, want := mermaidID("platform/auth-v2"), "platform_auth_v2"; got != want {
		t.Fatalf("mermaidID = %q, want %q", got, want)
	}
	if got, want := mermaidID("end"), "end_"; got != want {
		t.Fatalf("mermaidID keyword = %q, want %q", got, want)
	}
}
