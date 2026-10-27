package jig

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOrderedJSON(t *testing.T) {
	in := "{\n  \"zeta\": 1,\n  \"alpha\": {\"y\": [1, \"two\", {\"k\": null}], \"x\": true},\n  \"url\": \"https://a.b/<c>\"\n}\n"
	value, err := decodeJSON([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	root := value.(*jsonObject)
	want := "{\n  \"zeta\": 1,\n  \"alpha\": {\n    \"y\": [\n      1,\n      \"two\",\n      {\n        \"k\": null\n      }\n    ],\n    \"x\": true\n  },\n  \"url\": \"https://a.b/<c>\"\n}\n"
	if got := string(encodeJSON(root, "  ")); got != want {
		t.Fatalf("encode =\n%s\nwant\n%s", got, want)
	}
	if got := string(encodeJSON(root, "")); got != `{"zeta":1,"alpha":{"y":[1,"two",{"k":null}],"x":true},"url":"https://a.b/<c>"}` {
		t.Fatalf("compact = %s", got)
	}
	if err := root.setPath([]string{"mcpServers", "github"}, "v"); err != nil {
		t.Fatal(err)
	}
	if got, _ := root.getPath([]string{"mcpServers", "github"}); got != "v" || root.keys[len(root.keys)-1] != "mcpServers" {
		t.Fatalf("setPath: %v %v", got, root.keys)
	}
	root.removePath([]string{"mcpServers", "github"})
	if _, ok := root.get("mcpServers"); ok {
		t.Fatal("empty container not pruned")
	}
	if err := root.setPath([]string{"zeta", "x"}, 1); err == nil {
		t.Fatal("setPath through a non-object accepted")
	}
	if _, err := decodeJSON([]byte("{\n  // comment\n}")); err == nil {
		t.Fatal("JSONC accepted")
	}
}

func TestRenderMCPServer(t *testing.T) {
	local := MCPServer{Command: "npx", Args: []string{"-y", "@mcp/github"}, Env: map[string]string{"GITHUB_TOKEN": "${GITHUB_TOKEN}"}}
	remote := MCPServer{URL: "https://mcp.example.com/sse", Headers: map[string]string{"Authorization": "Bearer ${env:TOKEN}"}}
	cases := map[string][2]string{
		"claude":   {`{"type":"stdio","command":"npx","args":["-y","@mcp/github"],"env":{"GITHUB_TOKEN":"${GITHUB_TOKEN}"}}`, `{"type":"http","url":"https://mcp.example.com/sse","headers":{"Authorization":"Bearer ${TOKEN}"}}`},
		"vscode":   {`{"type":"stdio","command":"npx","args":["-y","@mcp/github"],"env":{"GITHUB_TOKEN":"${env:GITHUB_TOKEN}"}}`, `{"type":"http","url":"https://mcp.example.com/sse","headers":{"Authorization":"Bearer ${env:TOKEN}"}}`},
		"cursor":   {`{"type":"stdio","command":"npx","args":["-y","@mcp/github"],"env":{"GITHUB_TOKEN":"${env:GITHUB_TOKEN}"}}`, `{"type":"http","url":"https://mcp.example.com/sse","headers":{"Authorization":"Bearer ${env:TOKEN}"}}`},
		"copilot":  {`{"type":"local","command":"npx","args":["-y","@mcp/github"],"env":{"GITHUB_TOKEN":"${GITHUB_TOKEN}"},"tools":["*"]}`, `{"type":"http","url":"https://mcp.example.com/sse","headers":{"Authorization":"Bearer ${TOKEN}"}}`},
		"opencode": {`{"type":"local","command":["npx","-y","@mcp/github"],"environment":{"GITHUB_TOKEN":"{env:GITHUB_TOKEN}"},"enabled":true}`, `{"type":"remote","url":"https://mcp.example.com/sse","headers":{"Authorization":"Bearer {env:TOKEN}"},"enabled":true}`},
		"gemini":   {`{"command":"npx","args":["-y","@mcp/github"],"env":{"GITHUB_TOKEN":"${GITHUB_TOKEN}"}}`, `{"httpUrl":"https://mcp.example.com/sse","headers":{"Authorization":"Bearer ${TOKEN}"}}`},
		"kiro":     {`{"command":"npx","args":["-y","@mcp/github"],"env":{"GITHUB_TOKEN":"${GITHUB_TOKEN}"}}`, `{"url":"https://mcp.example.com/sse","headers":{"Authorization":"Bearer ${TOKEN}"}}`},
	}
	for shape, want := range cases {
		if got := string(encodeJSON(renderMCPServer(shape, local), "")); got != want[0] {
			t.Fatalf("%s local = %s\nwant %s", shape, got, want[0])
		}
		if got := string(encodeJSON(renderMCPServer(shape, remote), "")); got != want[1] {
			t.Fatalf("%s remote = %s\nwant %s", shape, got, want[1])
		}
	}
	tables := splitTOMLTables("model = \"o3\"\n\n[mcp_servers.a]\ncommand = \"x\"\n\n[other]\nk = 1\n")
	if len(tables) != 3 || tables[0].name != "" || tables[1].name != "mcp_servers.a" || tables[2].name != "other" {
		t.Fatalf("tables = %+v", tables)
	}
}

// Codex expands no reference: a variable reaches a server through the
// keys that name it, and a reference with no such key skips the server.
func TestRenderCodexTable(t *testing.T) {
	cases := []struct {
		name   string
		server MCPServer
		want   string
	}{
		{"local", MCPServer{Command: "npx", Args: []string{"-y", "@mcp/github"}, Env: map[string]string{"GITHUB_TOKEN": "${GITHUB_TOKEN}", "MODE": "read", "AWS_PROFILE": "${env:AWS_PROFILE}"}},
			"[mcp_servers.local]\ncommand = \"npx\"\nargs = [\"-y\", \"@mcp/github\"]\nenv = { MODE = \"read\" }\nenv_vars = [\"AWS_PROFILE\", \"GITHUB_TOKEN\"]\n"},
		{"bearer", MCPServer{URL: "https://mcp.example.com/sse", Headers: map[string]string{"authorization": "Bearer ${env:TOKEN}"}},
			"[mcp_servers.bearer]\nurl = \"https://mcp.example.com/sse\"\nbearer_token_env_var = \"TOKEN\"\n"},
		{"headers", MCPServer{URL: "https://mcp.example.com/sse", Headers: map[string]string{"X-Api-Key": "${API_KEY}", "X-Team": "finca"}},
			"[mcp_servers.headers]\nurl = \"https://mcp.example.com/sse\"\nhttp_headers = { X-Team = \"finca\" }\nenv_http_headers = { X-Api-Key = \"API_KEY\" }\n"},
	}
	for _, c := range cases {
		got, err := renderCodexTable(c.name, c.server)
		if err != nil || got != c.want {
			t.Fatalf("%s = %v\n%s\nwant\n%s", c.name, err, got, c.want)
		}
	}
	unsupported := map[string]MCPServer{
		"variable renamed":    {Command: "x", Env: map[string]string{"GITHUB_PERSONAL_ACCESS_TOKEN": "${GITHUB_PAT}"}},
		"reference in value":  {Command: "x", Env: map[string]string{"DSN": "postgres://${USER}@db"}},
		"reference in args":   {Command: "x", Args: []string{"--token", "${TOKEN}"}},
		"reference in url":    {URL: "https://${HOST}/mcp"},
		"reference in header": {URL: "https://mcp.example.com", Headers: map[string]string{"X-Auth": "Token ${TOKEN}"}},
		"bearer elsewhere":    {URL: "https://mcp.example.com", Headers: map[string]string{"X-Auth": "Bearer ${TOKEN}"}},
	}
	for label, server := range unsupported {
		if got, err := renderCodexTable("s", server); err == nil {
			t.Fatalf("%s rendered:\n%s", label, got)
		}
	}
}

// A server Codex cannot take is skipped in its document and reported,
// and still written for the harnesses that expand references.
func TestCodexSkipsServerWithoutCodexForm(t *testing.T) {
	root := t.TempDir()
	t.Setenv("JIG_CACHE_DIR", filepath.Join(root, "cache"))
	writeTestWorkspace(t, root, `{"version": 4, "agents": {"mcp": {
	  "github": {"command": "github-mcp-server", "args": ["stdio"], "env": {"GITHUB_PERSONAL_ACCESS_TOKEN": "${GITHUB_PAT}"}},
	  "aws": {"command": "uvx", "args": ["awslabs.aws-api-mcp-server"], "env": {"AWS_PROFILE": "${AWS_PROFILE}"}}
	}}, "tree": {}}`)
	harnesses := []string{"codex", "opencode"}
	if err := saveConfig(root, Config{Version: 1, Schema: "jig.json", Harnesses: &harnesses}); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	var out bytes.Buffer
	if err := Clone(CloneOptions{}, &out); err != nil {
		t.Fatalf("clone: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "wrote-file: .codex/config.toml (1 managed keys)\n  skipped mcp server github: ") {
		t.Fatalf("report:\n%s", out.String())
	}
	codex, err := os.ReadFile(filepath.Join(root, ".codex", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(codex), "github") || !strings.Contains(string(codex), "env_vars = [\"AWS_PROFILE\"]") {
		t.Fatalf(".codex/config.toml =\n%s", codex)
	}
	opencode, err := os.ReadFile(filepath.Join(root, "opencode.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(opencode), "\"GITHUB_PERSONAL_ACCESS_TOKEN\": \"{env:GITHUB_PAT}\"") {
		t.Fatalf("opencode.json =\n%s", opencode)
	}
}

func TestValidateMCP(t *testing.T) {
	def := testDefinition(t, `{"version": 4, "agents": {"mcp": {
	  "bad name": {"command": "x"},
	  "both": {"command": "x", "url": "https://y"},
	  "none": {},
	  "gated": {"url": "https://y", "onlyWhen": {"tags": ["nope"]}},
	  "ok": {"command": "x", "args": ["a"]}
	}}, "tree": {}}`)
	errs := validateDefinition(def).Errors
	joined := strings.Join(errs, "\n")
	for _, want := range []string{`mcp server "bad name" has an invalid name`, "mcp server both must define exactly one", "mcp server none must define exactly one", "mcp server gated onlyWhen tags nope do not match"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in:\n%s", want, joined)
		}
	}
	if len(errs) != 4 {
		t.Fatalf("errors = %v", errs)
	}
	// The derived documents are never reported for lacking sources.
	model, err := flattenDefinition(testDefinition(t, `{"version": 4, "agents": {"mcp": {"ok": {"command": "x"}}}, "tree": {}}`), []string{"claude", "copilot", "windsurf"})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(sortedEntryPaths(&model), " "); got != ".github/mcp.json .mcp.json .vscode/mcp.json" {
		t.Fatalf("entries = %s", got)
	}
	if id := model.Entries[".vscode/mcp.json"].Identity; id != "agents-mcp-copilot-vscode" {
		t.Fatalf("identity = %s", id)
	}
	model, _ = flattenDefinition(testDefinition(t, `{"version": 4, "agents": {"mcp": {"ok": {"command": "x"}}}, "tree": {}}`), nil)
	if len(model.Entries) != 0 {
		t.Fatalf("no harness derived %v", sortedEntryPaths(&model))
	}
}

// End to end across the JSON and TOML documents: a user's own servers
// survive, a conflicting one is kept, an owned key the user edits is kept
// and reported, a server removed from the schema leaves every document,
// and a document jig created is deleted once empty.
func TestAgentsBlockMaterializesMCP(t *testing.T) {
	root := t.TempDir()
	t.Setenv("JIG_CACHE_DIR", filepath.Join(root, "cache"))
	schema := func(withBilling bool) string {
		billing := ""
		if withBilling {
			billing = `, "billing": {"url": "https://mcp.example.com/sse", "headers": {"Authorization": "Bearer ${TOKEN}"}}`
		}
		return fmt.Sprintf(`{"version": 4, "agents": {"mcp": {
		  "github": {"command": "npx", "args": ["-y", "@mcp/github"], "env": {"GITHUB_TOKEN": "${GITHUB_TOKEN}"}}%s
		}}, "tree": {}}`, billing)
	}
	writeTestWorkspace(t, root, schema(true))
	harnesses := []string{"claude", "copilot", "opencode", "codex", "gemini"}
	if err := saveConfig(root, Config{Version: 1, Schema: "jig.json", Harnesses: &harnesses}); err != nil {
		t.Fatal(err)
	}
	// A pre-existing Claude document with the user's own server and a
	// conflicting github entry, and a Codex config with other settings.
	if err := os.WriteFile(filepath.Join(root, ".mcp.json"), []byte("{\n  \"mcpServers\": {\n    \"mine\": {\"command\": \"my-server\"},\n    \"github\": {\"command\": \"their-github\"}\n  }\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".codex", "config.toml"), []byte("# my codex config\nmodel = \"o3\"\n"), 0o644); err != nil {
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
	claude := read(".mcp.json")
	if !strings.Contains(claude, "\"their-github\"") || !strings.Contains(claude, "\"mine\"") || !strings.Contains(claude, "\"billing\"") || strings.Index(claude, "mine") > strings.Index(claude, "billing") {
		t.Fatalf(".mcp.json =\n%s", claude)
	}
	if !strings.Contains(out.String(), "updated-file: .mcp.json (1 managed keys, 1 conflicting kept)") {
		t.Fatalf("claude report:\n%s", out.String())
	}
	if got := read(".vscode/mcp.json"); !strings.Contains(got, "\"servers\"") || !strings.Contains(got, "${env:GITHUB_TOKEN}") || !strings.Contains(got, "\"type\": \"stdio\"") {
		t.Fatalf(".vscode/mcp.json =\n%s", got)
	}
	if got := read(".github/mcp.json"); !strings.Contains(got, "\"tools\": [\n        \"*\"\n      ]") {
		t.Fatalf(".github/mcp.json =\n%s", got)
	}
	if got := read("opencode.json"); !strings.Contains(got, "\"command\": [\n        \"npx\",") || !strings.Contains(got, "\"type\": \"remote\"") {
		t.Fatalf("opencode.json =\n%s", got)
	}
	if got := read(".gemini/settings.json"); !strings.Contains(got, "\"httpUrl\"") {
		t.Fatalf(".gemini/settings.json =\n%s", got)
	}
	codex := read(".codex/config.toml")
	if !strings.HasPrefix(codex, "# my codex config\nmodel = \"o3\"\n\n[mcp_servers.billing]\n") || !strings.Contains(codex, "\n[mcp_servers.github]\ncommand = \"npx\"\n") {
		t.Fatalf(".codex/config.toml =\n%s", codex)
	}

	out.Reset()
	if err := Sync(SyncOptions{SkipUpdate: true}, &out); err != nil {
		t.Fatalf("sync: %v\n%s", err, out.String())
	}
	if strings.Count(out.String(), "present-file:") != 6 {
		t.Fatalf("second run:\n%s", out.String())
	}

	// The user edits an owned key in VS Code's document: kept, reported.
	vscode := strings.Replace(read(".vscode/mcp.json"), "\"-y\"", "\"--yes\"", 1)
	if err := os.WriteFile(filepath.Join(root, ".vscode", "mcp.json"), []byte(vscode), 0o644); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := Sync(SyncOptions{SkipUpdate: true}, &out); err != nil {
		t.Fatalf("sync after edit: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "present-file: .vscode/mcp.json (1 managed keys, 1 modified kept)") || !strings.Contains(read(".vscode/mcp.json"), "--yes") {
		t.Fatalf("edit kept:\n%s", out.String())
	}
	var status bytes.Buffer
	if err := Status(StatusOptions{}, &status); err != nil {
		t.Fatalf("status: %v\n%s", err, status.String())
	}
	if !strings.Contains(status.String(), ".vscode/mcp.json") || !strings.Contains(status.String(), "1 modified") {
		t.Fatalf("status:\n%s", status.String())
	}

	// Billing leaves the schema: removed everywhere it was jig's; the
	// github key the user edited stays and is reported.
	if err := os.WriteFile(filepath.Join(root, sourceDir, "jig.json"), []byte(schema(false)), 0o644); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := Sync(SyncOptions{SkipUpdate: true}, &out); err != nil {
		t.Fatalf("sync after removal: %v\n%s", err, out.String())
	}
	for _, rel := range []string{".mcp.json", ".vscode/mcp.json", "opencode.json", ".gemini/settings.json", ".codex/config.toml"} {
		if strings.Contains(read(rel), "billing") {
			t.Fatalf("billing survived in %s:\n%s", rel, read(rel))
		}
	}
	if !strings.Contains(read(".codex/config.toml"), "# my codex config\nmodel = \"o3\"\n") || !strings.Contains(read(".mcp.json"), "\"mine\"") {
		t.Fatal("user content lost")
	}

	// Everything leaves the schema: documents jig created and emptied go,
	// the user's documents keep their own content, and the edited key is
	// left untracked.
	if err := os.WriteFile(filepath.Join(root, sourceDir, "jig.json"), []byte(`{"version": 4, "agents": {"mcp": {}}, "tree": {}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := Sync(SyncOptions{SkipUpdate: true, Prune: true}, &out); err != nil {
		t.Fatalf("sync prune: %v\n%s", err, out.String())
	}
	for _, rel := range []string{".github/mcp.json", "opencode.json", ".gemini/settings.json"} {
		if pathExists(filepath.Join(root, filepath.FromSlash(rel))) {
			t.Fatalf("%s not deleted:\n%s", rel, out.String())
		}
	}
	if got := read(".mcp.json"); strings.Contains(got, "github") && !strings.Contains(got, "their-github") || !strings.Contains(got, "mine") {
		t.Fatalf(".mcp.json after prune =\n%s", got)
	}
	if got := read(".vscode/mcp.json"); !strings.Contains(got, "--yes") {
		t.Fatalf("edited key removed:\n%s", got)
	}
	if got := read(".codex/config.toml"); strings.Contains(got, "mcp_servers") || !strings.Contains(got, "model = \"o3\"") {
		t.Fatalf(".codex/config.toml after prune =\n%s", got)
	}
}
