package jig

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderHook(t *testing.T) {
	tool := Hook{Event: "afterToolUse", Matcher: "Edit|Write", Command: "jig fmt", Timeout: 5}
	stop := Hook{Event: "stop", Command: "echo done"}
	render := func(format string, hook Hook) (string, string) {
		t.Helper()
		elements, reason := renderHook(format, format, hook)
		if reason != "" {
			return "", reason
		}
		var parts []string
		for _, element := range elements {
			parts = append(parts, element.event+" "+string(encodeJSON(element.value, "")))
		}
		return strings.Join(parts, "; "), ""
	}
	cases := map[string][2]string{
		"claude":   {`PostToolUse {"matcher":"Edit|Write","hooks":[{"type":"command","command":"jig fmt","timeout":5}]}`, `Stop {"hooks":[{"type":"command","command":"echo done"}]}`},
		"codex":    {`PostToolUse {"matcher":"Edit|Write","hooks":[{"type":"command","command":"jig fmt","timeout":5}]}`, `Stop {"hooks":[{"type":"command","command":"echo done"}]}`},
		"gemini":   {`AfterTool {"matcher":"replace|write_file","hooks":[{"type":"command","command":"jig fmt","timeout":5000}]}`, `AfterAgent {"hooks":[{"type":"command","command":"echo done"}]}`},
		"cursor":   {`postToolUse {"command":"jig fmt","timeout":5,"matcher":"Write"}`, `stop {"command":"echo done"}`},
		"windsurf": {`post_write_code {"command":"jig fmt"}`, `post_cascade_response {"command":"echo done"}`},
	}
	for format, want := range cases {
		if got, reason := render(format, tool); got != want[0] || reason != "" {
			t.Fatalf("%s tool = %s %s\nwant %s", format, got, reason, want[0])
		}
		if got, reason := render(format, stop); got != want[1] || reason != "" {
			t.Fatalf("%s stop = %s %s\nwant %s", format, got, reason, want[1])
		}
	}
	if got, _ := render("copilot", stop); got != `agentStop {"type":"command","bash":"echo done"}` {
		t.Fatalf("copilot stop = %s", got)
	}
	if _, reason := render("copilot", tool); reason != "copilot hooks have no tool matcher" {
		t.Fatalf("copilot tool reason = %q", reason)
	}
	if got, _ := render("copilot", Hook{Event: "beforeToolUse", Command: "check", Timeout: 2}); got != `preToolUse {"type":"command","bash":"check","timeoutSec":2}` {
		t.Fatalf("copilot tool = %s", got)
	}
	if _, reason := render("windsurf", Hook{Event: "sessionStart", Command: "x"}); reason != "windsurf has no sessionStart event" {
		t.Fatalf("windsurf sessionStart reason = %q", reason)
	}
	if got, _ := render("windsurf", Hook{Event: "beforeToolUse", Command: "x"}); got != `pre_run_command {"command":"x"}; pre_write_code {"command":"x"}; pre_mcp_tool_use {"command":"x"}; pre_read_code {"command":"x"}` {
		t.Fatalf("windsurf unmatched = %s", got)
	}
	if got, _ := render("windsurf", Hook{Event: "beforeToolUse", Matcher: "Bash|mcp__github__search", Command: "x"}); got != `pre_run_command {"command":"x"}; pre_mcp_tool_use {"command":"x"}` {
		t.Fatalf("windsurf matched = %s", got)
	}
	if _, reason := render("windsurf", Hook{Event: "beforeToolUse", Matcher: "Bash.*", Command: "x"}); !strings.HasPrefix(reason, "windsurf cannot express matcher") {
		t.Fatalf("windsurf pattern reason = %q", reason)
	}
	// Patterns and unknown names pass through where the harness matches
	// on its own names; translated names are deduplicated.
	if got := translateMatcher("Bash|Edit.*", cursorTools); got != "Bash|Edit.*" {
		t.Fatalf("pattern = %s", got)
	}
	if got := translateMatcher("Edit|Write|Bash|Custom", cursorTools); got != "Write|Shell|Custom" {
		t.Fatalf("translated = %s", got)
	}
	// Elements are owned by what they do: the identity survives a round
	// trip through the document and ignores the rest of the element.
	claude, _ := renderHook("claude", "claude", tool)
	decoded, _ := decodeJSON(encodeJSON(claude[0].value, "  "))
	edited, _ := renderHook("claude", "claude", Hook{Event: "afterToolUse", Matcher: "Edit|Write", Command: "jig fmt", Timeout: 9})
	if hookIdentity(decoded) != hookIdentity(claude[0].value) || hookIdentity(edited[0].value) != hookIdentity(claude[0].value) {
		t.Fatal("identity unstable")
	}
	other, _ := renderHook("claude", "claude", Hook{Event: "afterToolUse", Matcher: "Edit", Command: "jig fmt"})
	if hookIdentity(other[0].value) == hookIdentity(claude[0].value) {
		t.Fatal("identity ignores the matcher")
	}
}

func TestValidateHooks(t *testing.T) {
	def := testDefinition(t, `{"version": 4, "agents": {"hooks": [
	  {"event": "onSave", "command": "x"},
	  {"event": "stop", "matcher": "Edit", "command": "x"},
	  {"event": "stop"},
	  {"event": "beforeToolUse", "command": "x", "timeout": -1},
	  {"event": "sessionStart", "command": "x", "onlyWhen": {"tags": ["nope"]}},
	  {"event": "afterToolUse", "matcher": "Edit|Write", "command": "jig fmt", "timeout": 5}
	]}, "tree": {}}`)
	errs := validateDefinition(def).Errors
	joined := strings.Join(errs, "\n")
	for _, want := range []string{`hook 1 has an unknown event "onSave"`, "hook 2: a matcher applies to beforeToolUse and afterToolUse only", "hook 3 has no command", "hook 4 has a negative timeout", "hook 5 onlyWhen tags nope do not match"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in:\n%s", want, joined)
		}
	}
	if len(errs) != 5 {
		t.Fatalf("errors = %v", errs)
	}
	model, err := flattenDefinition(testDefinition(t, `{"version": 4, "agents": {"hooks": [{"event": "stop", "command": "x"}]}, "tree": {}}`), []string{"claude", "copilot", "gemini", "opencode", "kiro"})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(sortedEntryPaths(&model), " "); got != ".claude/settings.json .gemini/settings.json .github/hooks/jig.json" {
		t.Fatalf("entries = %s", got)
	}
	if id := model.Entries[".gemini/settings.json"].Identity; id != "agents-mcp-gemini" {
		t.Fatalf("gemini identity = %s", id)
	}
	if id := model.Entries[".claude/settings.json"].Identity; id != "agents-hooks-claude" {
		t.Fatalf("claude identity = %s", id)
	}
}

// End to end across the hook documents: a user's own settings survive, a
// conflicting element is kept, an owned element the user edits is kept
// and reported, a hook removed from the schema leaves every document, a
// harness without an equivalent reports the skipped hook, and documents
// jig created are deleted once empty.
func TestAgentsBlockMaterializesHooks(t *testing.T) {
	root := t.TempDir()
	t.Setenv("JIG_CACHE_DIR", filepath.Join(root, "cache"))
	schema := func(withStop bool) string {
		stop := ""
		if withStop {
			stop = `, {"event": "stop", "command": "echo done"}`
		}
		return fmt.Sprintf(`{"version": 4, "agents": {
		  "mcp": {"github": {"command": "npx", "args": ["-y", "@mcp/github"]}},
		  "hooks": [
		    {"event": "afterToolUse", "matcher": "Edit|Write", "command": "jig fmt", "timeout": 5},
		    {"event": "sessionStart", "command": "echo hi"},
		    {"event": "beforePrompt", "command": "echo gated", "onlyWhen": {"tags": ["nope"]}}%s
		  ]
		}, "tree": {}}`, stop)
	}
	writeTestWorkspace(t, root, schema(true))
	harnesses := []string{"claude", "copilot", "cursor", "gemini", "windsurf", "codex"}
	if err := saveConfig(root, Config{Version: 1, Schema: "jig.json", Harnesses: &harnesses}); err != nil {
		t.Fatal(err)
	}
	// A pre-existing Claude settings file with the user's permissions and
	// their own version of the stop hook.
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	userSettings := "{\n  \"permissions\": {\"allow\": [\"Bash(git:*)\"]},\n  \"hooks\": {\n    \"Stop\": [\n      {\"hooks\": [{\"type\": \"command\", \"command\": \"echo done\", \"timeout\": 99}]}\n    ]\n  }\n}\n"
	if err := os.WriteFile(filepath.Join(root, ".claude", "settings.json"), []byte(userSettings), 0o644); err != nil {
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
	claude := read(".claude/settings.json")
	if !strings.HasPrefix(claude, "{\n  \"permissions\"") || !strings.Contains(claude, "\"timeout\": 99") || !strings.Contains(claude, "\"PostToolUse\"") || !strings.Contains(claude, "\"SessionStart\"") || strings.Contains(claude, "gated") {
		t.Fatalf(".claude/settings.json =\n%s", claude)
	}
	if !strings.Contains(out.String(), "updated-file: .claude/settings.json (2 managed keys, 1 conflicting kept)") {
		t.Fatalf("claude report:\n%s", out.String())
	}
	cursor := read(".cursor/hooks.json")
	if !strings.HasPrefix(cursor, "{\n  \"version\": 1,\n  \"hooks\": {\n    \"postToolUse\": [\n      {\n        \"command\": \"jig fmt\",\n        \"timeout\": 5,\n        \"matcher\": \"Write\"\n      }\n    ],") || !strings.Contains(cursor, "\"stop\"") {
		t.Fatalf(".cursor/hooks.json =\n%s", cursor)
	}
	if got := read(".github/hooks/jig.json"); !strings.Contains(got, "\"agentStop\"") || !strings.Contains(got, "\"bash\": \"echo done\"") || strings.Contains(got, "jig fmt") {
		t.Fatalf(".github/hooks/jig.json =\n%s", got)
	}
	if !strings.Contains(out.String(), "wrote-file: .github/hooks/jig.json (3 managed keys)\n  skipped hook afterToolUse \"jig fmt\": copilot hooks have no tool matcher") {
		t.Fatalf("copilot report:\n%s", out.String())
	}
	gemini := read(".gemini/settings.json")
	if !strings.Contains(gemini, "\"mcpServers\"") || !strings.Contains(gemini, "\"AfterTool\"") || !strings.Contains(gemini, "\"matcher\": \"replace|write_file\"") || !strings.Contains(gemini, "\"timeout\": 5000") || !strings.Contains(gemini, "\"AfterAgent\"") {
		t.Fatalf(".gemini/settings.json =\n%s", gemini)
	}
	if got := read(".windsurf/hooks.json"); !strings.Contains(got, "\"post_write_code\"") || !strings.Contains(got, "\"post_cascade_response\"") || strings.Contains(got, "echo hi") {
		t.Fatalf(".windsurf/hooks.json =\n%s", got)
	}
	if !strings.Contains(out.String(), "skipped hook sessionStart \"echo hi\": windsurf has no sessionStart event") {
		t.Fatalf("windsurf report:\n%s", out.String())
	}
	if got := read(".codex/hooks.json"); !strings.Contains(got, "\"PostToolUse\"") {
		t.Fatalf(".codex/hooks.json =\n%s", got)
	}

	out.Reset()
	if err := Sync(SyncOptions{SkipUpdate: true}, &out); err != nil {
		t.Fatalf("sync: %v\n%s", err, out.String())
	}
	if strings.Count(out.String(), "present-file:") != 11 || strings.Contains(out.String(), "wrote-file") || strings.Contains(out.String(), "updated-file") {
		t.Fatalf("second run:\n%s", out.String())
	}

	// The user edits an owned element in Cursor's document: kept, reported.
	if err := os.WriteFile(filepath.Join(root, ".cursor", "hooks.json"), []byte(strings.Replace(cursor, "\"timeout\": 5", "\"timeout\": 7", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := Sync(SyncOptions{SkipUpdate: true}, &out); err != nil {
		t.Fatalf("sync after edit: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "present-file: .cursor/hooks.json (3 managed keys, 1 modified kept)") || !strings.Contains(read(".cursor/hooks.json"), "\"timeout\": 7") {
		t.Fatalf("edit kept:\n%s", out.String())
	}
	var status bytes.Buffer
	if err := Status(StatusOptions{}, &status); err != nil {
		t.Fatalf("status: %v\n%s", err, status.String())
	}
	if !strings.Contains(status.String(), ".cursor/hooks.json") || !strings.Contains(status.String(), "1 modified") {
		t.Fatalf("status:\n%s", status.String())
	}

	// The stop hook leaves the schema: removed everywhere it was jig's; the
	// user's own stop hook in Claude's settings stays.
	if err := os.WriteFile(filepath.Join(root, sourceDir, "jig.json"), []byte(schema(false)), 0o644); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := Sync(SyncOptions{SkipUpdate: true}, &out); err != nil {
		t.Fatalf("sync after removal: %v\n%s", err, out.String())
	}
	for _, rel := range []string{".cursor/hooks.json", ".gemini/settings.json", ".windsurf/hooks.json", ".codex/hooks.json"} {
		if strings.Contains(read(rel), "echo done") {
			t.Fatalf("stop survived in %s:\n%s", rel, read(rel))
		}
	}
	if !strings.Contains(read(".claude/settings.json"), "\"timeout\": 99") {
		t.Fatal("user's stop hook lost")
	}
	if got := read(".github/hooks/jig.json"); strings.Contains(got, "echo done") || !strings.Contains(got, "\"sessionStart\"") {
		t.Fatalf(".github/hooks/jig.json after removal =\n%s", got)
	}

	// Everything leaves the schema: documents jig created and emptied go,
	// the user's document keeps its content, and the document with the
	// edited element is locally modified, so prune keeps it whole.
	if err := os.WriteFile(filepath.Join(root, sourceDir, "jig.json"), []byte(`{"version": 4, "agents": {"hooks": []}, "tree": {}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := Sync(SyncOptions{SkipUpdate: true, Prune: true}, &out); err != nil {
		t.Fatalf("sync prune: %v\n%s", err, out.String())
	}
	for _, rel := range []string{".gemini/settings.json", ".windsurf/hooks.json", ".codex/hooks.json", ".github/hooks/jig.json"} {
		if pathExists(filepath.Join(root, filepath.FromSlash(rel))) {
			t.Fatalf("%s not deleted:\n%s", rel, out.String())
		}
	}
	if got := read(".claude/settings.json"); !strings.Contains(got, "\"permissions\"") || !strings.Contains(got, "\"timeout\": 99") || strings.Contains(got, "PostToolUse") {
		t.Fatalf(".claude/settings.json after prune =\n%s", got)
	}
	if got := read(".cursor/hooks.json"); !strings.Contains(got, "\"timeout\": 7") || !strings.Contains(out.String(), "locally modified: hooks/postToolUse#") {
		t.Fatalf(".cursor/hooks.json after prune =\n%s\n%s", got, out.String())
	}
}
