package jig

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// MCPServer is one server of the agents block's mcp map, in the standard
// shape: command, args, env for a local server, url and headers for a
// remote one. type is accepted on input (plugins write it) and otherwise
// derived from which of command and url is set.
type MCPServer struct {
	Type     string            `json:"type,omitempty"`
	Command  string            `json:"command,omitempty"`
	Args     []string          `json:"args,omitempty"`
	Env      map[string]string `json:"env,omitempty"`
	URL      string            `json:"url,omitempty"`
	Headers  map[string]string `json:"headers,omitempty"`
	OnlyWhen *Condition        `json:"onlyWhen,omitempty"`
	scope    string            // from a group's agents block: the group path gating the server
	fallback *MCPServer        // the server of the same name this one overrides, used while this one is gated off
}

func (s MCPServer) remote() bool { return s.URL != "" }

var mcpServerName = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

// managedDoc makes a $file entry a managed document: a configuration file
// in which jig owns the keys it wrote and nothing else. It is derived per
// harness document from the mcp map and the hooks list, and from the
// plugins' .mcp.json and hooks/hooks.json files. A document carries
// servers when shape is set, hooks when hookFormat is set, or both.
type managedDoc struct {
	harness    string
	shape      string // how servers are rendered (see renderMCPServer); "" for no servers
	hookFormat string // how hooks are rendered (see hookFormats); "" for no hooks
	servers    map[string]MCPServer
	hooks      []Hook
	plugins    SrcList // plugin sources whose files feed the document
}

// managedDocument is one harness's configuration file in the matrix.
type managedDocument struct {
	path   string
	shape  string // MCP shape, "" when the document carries no servers
	hooks  string // hook format, "" when it carries no hooks
	suffix string // identity suffix when a harness has several MCP documents
}

// managedDocuments is the mcp and hooks rows of the matrix, per harness.
// Windsurf reads MCP servers from a global file only; OpenCode has no
// command hooks and Kiro's hooks are agent triggers, so those cells are
// absent. Gemini keeps both in one settings file: one entry owns both.
var managedDocuments = map[string][]managedDocument{
	"claude":   {{path: ".mcp.json", shape: "claude"}, {path: ".claude/settings.json", hooks: "claude"}},
	"codex":    {{path: ".codex/config.toml", shape: "codex"}, {path: ".codex/hooks.json", hooks: "codex"}},
	"copilot":  {{path: ".github/mcp.json", shape: "copilot"}, {path: ".vscode/mcp.json", shape: "vscode", suffix: "vscode"}, {path: ".github/hooks/jig.json", hooks: "copilot"}},
	"cursor":   {{path: ".cursor/mcp.json", shape: "cursor"}, {path: ".cursor/hooks.json", hooks: "cursor"}},
	"opencode": {{path: "opencode.json", shape: "opencode"}},
	"gemini":   {{path: ".gemini/settings.json", shape: "gemini", hooks: "gemini"}},
	"kiro":     {{path: ".kiro/settings/mcp.json", shape: "kiro"}},
	"windsurf": {{path: ".windsurf/hooks.json", hooks: "windsurf"}},
}

const (
	agentsMCPIdentity   = "agents-mcp"
	agentsHooksIdentity = "agents-hooks"
)

// expandManaged derives one managed document per harness document in
// use that has something to carry. There is no shared document, so
// nothing is canonical and nothing is derived when no harness is listed.
func expandManaged(block *AgentsBlock, harnesses []string, model *Model) error {
	servers := len(block.MCP) > 0 || len(block.Plugins) > 0
	hooks := len(block.Hooks) > 0 || len(block.Plugins) > 0
	for _, harness := range harnesses {
		for _, doc := range managedDocuments[harness] {
			carries := (doc.shape != "" && servers) || (doc.hooks != "" && hooks)
			if !carries {
				continue
			}
			identity := agentsHooksIdentity + "-" + harness
			if doc.shape != "" {
				identity = agentsMCPIdentity + "-" + harness
			}
			if doc.suffix != "" {
				identity += "-" + doc.suffix
			}
			what := "MCP servers"
			switch {
			case doc.shape != "" && doc.hooks != "":
				what = "MCP servers and hooks"
			case doc.hooks != "":
				what = "Hooks"
			}
			managed := &managedDoc{harness: harness, shape: doc.shape, hookFormat: doc.hooks, plugins: pluginSources(block.Plugins, "mcp")}
			if doc.shape != "" {
				managed.servers = block.MCP
			}
			if doc.hooks != "" {
				managed.hooks = block.Hooks
			}
			entry := Entry{Path: doc.path, Identity: identity, Kind: EntryFile, File: &File{
				ID:          identity,
				Description: fmt.Sprintf("%s from the agents block, as %s reads them", what, harness),
				managed:     managed,
			}}
			if err := addDerived(model, entry); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateMCP checks the mcp map: valid names, exactly one transport,
// satisfiable conditions.
func validateMCP(result *validationResult, model *Model, servers map[string]MCPServer) {
	for _, name := range sortedMCPNames(servers) {
		server := servers[name]
		if !mcpServerName.MatchString(name) {
			result.Errors = append(result.Errors, fmt.Sprintf("mcp server %q has an invalid name: use letters, digits, _ . -", name))
		}
		if (server.Command != "") == (server.URL != "") {
			result.Errors = append(result.Errors, fmt.Sprintf("mcp server %s must define exactly one of command and url", name))
		}
		if server.OnlyWhen != nil {
			validateCondition(result, model, "mcp server "+name, *server.OnlyWhen)
		}
	}
}

func sortedMCPNames(servers map[string]MCPServer) []string {
	names := make([]string, 0, len(servers))
	for name := range servers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Environment references: the neutral form is ${NAME}, and ${env:NAME} is
// accepted too. Every document gets its harness's form whichever one the
// schema used: ${env:NAME} for VS Code and Cursor, {env:NAME} for
// OpenCode, ${NAME} for the others. Codex expands no reference at all;
// see renderCodexTable.
var envReference = regexp.MustCompile(`\$\{(?:env:)?([A-Za-z_][A-Za-z0-9_]*)\}`)

type envStyle int

const (
	envDollar   envStyle = iota // ${NAME}
	envVSCode                   // ${env:NAME}
	envOpenCode                 // {env:NAME}
)

func shapeEnvStyle(shape string) envStyle {
	switch shape {
	case "vscode", "cursor":
		return envVSCode
	case "opencode":
		return envOpenCode
	}
	return envDollar
}

func translateEnvRefs(value string, style envStyle) string {
	return envReference.ReplaceAllStringFunc(value, func(match string) string {
		name := envReference.FindStringSubmatch(match)[1]
		switch style {
		case envVSCode:
			return "${env:" + name + "}"
		case envOpenCode:
			return "{env:" + name + "}"
		}
		return "${" + name + "}"
	})
}

func translateServer(server MCPServer, style envStyle) MCPServer {
	tr := func(s string) string { return translateEnvRefs(s, style) }
	out := server
	out.Command = tr(server.Command)
	out.URL = tr(server.URL)
	out.Args = nil
	for _, arg := range server.Args {
		out.Args = append(out.Args, tr(arg))
	}
	out.Env, out.Headers = nil, nil
	if len(server.Env) > 0 {
		out.Env = map[string]string{}
		for k, v := range server.Env {
			out.Env[k] = tr(v)
		}
	}
	if len(server.Headers) > 0 {
		out.Headers = map[string]string{}
		for k, v := range server.Headers {
			out.Headers[k] = tr(v)
		}
	}
	return out
}

// mcpContainer is the key the servers live under in each JSON shape.
func mcpContainer(shape string) string {
	switch shape {
	case "vscode":
		return "servers"
	case "opencode":
		return "mcp"
	}
	return "mcpServers"
}

// renderMCPServer builds a harness's server object. The shapes follow the
// harnesses' documentation and APM's client adapters.
func renderMCPServer(shape string, server MCPServer) *jsonObject {
	server = translateServer(server, shapeEnvStyle(shape))
	o := newJSONObject()
	switch shape {
	case "opencode":
		if server.remote() {
			o.set("type", "remote")
			o.set("url", server.URL)
			if len(server.Headers) > 0 {
				o.set("headers", jsonMap(server.Headers))
			}
		} else {
			o.set("type", "local")
			o.set("command", jsonList(append([]string{server.Command}, server.Args...)))
			if len(server.Env) > 0 {
				o.set("environment", jsonMap(server.Env))
			}
		}
		o.set("enabled", true)
		return o
	case "claude", "vscode", "cursor", "copilot":
		if server.remote() {
			o.set("type", "http")
		} else if shape == "copilot" {
			o.set("type", "local")
		} else {
			o.set("type", "stdio")
		}
	}
	if server.remote() {
		if shape == "gemini" {
			o.set("httpUrl", server.URL)
		} else {
			o.set("url", server.URL)
		}
		if len(server.Headers) > 0 {
			o.set("headers", jsonMap(server.Headers))
		}
	} else {
		o.set("command", server.Command)
		if len(server.Args) > 0 {
			o.set("args", jsonList(server.Args))
		}
		if len(server.Env) > 0 {
			o.set("env", jsonMap(server.Env))
		}
		if shape == "copilot" {
			o.set("tools", jsonList([]string{"*"}))
		}
	}
	return o
}

// Managed keys are named by path, "a/b/c" for a nested key, and an element
// of an array by its container's path and its identity, "hooks/Stop#id":
// state and messages use these names, and the item helpers below read and
// write a document by them.
func keyPath(path []string) string { return strings.Join(path, "/") }

func splitKeyPath(key string) []string { return strings.Split(key, "/") }

func splitItemKey(key string) (path []string, id string) {
	if i := strings.LastIndex(key, "#"); i >= 0 {
		return splitKeyPath(key[:i]), key[i+1:]
	}
	return splitKeyPath(key), ""
}

// getItem finds a key's value, or the element of the array with the
// key's identity.
func getItem(root *jsonObject, key string) (any, bool) {
	path, id := splitItemKey(key)
	value, ok := root.getPath(path)
	if !ok || id == "" {
		return value, ok
	}
	list, _ := value.([]any)
	for _, element := range list {
		if hookIdentity(element) == id {
			return element, true
		}
	}
	return nil, false
}

// setItem assigns a key, or replaces the array element with the key's
// identity in place, appending it when new.
func setItem(root *jsonObject, key string, value any) error {
	path, id := splitItemKey(key)
	if id == "" {
		return root.setPath(path, value)
	}
	current, ok := root.getPath(path)
	if !ok {
		return root.setPath(path, []any{value})
	}
	list, isList := current.([]any)
	if !isList {
		return fmt.Errorf("%s is not an array", keyPath(path))
	}
	for i, element := range list {
		if hookIdentity(element) == id {
			list[i] = value
			return root.setPath(path, list)
		}
	}
	return root.setPath(path, append(list, value))
}

// removeItem deletes a key, or the array element with the key's identity,
// and the containers that leaves empty.
func removeItem(root *jsonObject, key string) {
	path, id := splitItemKey(key)
	if id == "" {
		root.removePath(path)
		return
	}
	current, ok := root.getPath(path)
	list, isList := current.([]any)
	if !ok || !isList {
		return
	}
	var kept []any
	for _, element := range list {
		if hookIdentity(element) != id {
			kept = append(kept, element)
		}
	}
	if len(kept) == 0 {
		root.removePath(path)
		return
	}
	_ = root.setPath(path, kept)
}

// activeContent collects what the document carries: the plugins' servers
// and hooks first, then the block's, so an explicit declaration wins on
// a name or identity; each gated by its condition. skipped lists the
// plugin hooks with no neutral equivalent.
func (m *materializer) activeContent(doc *managedDoc) (servers map[string]MCPServer, hooks []Hook, skipped []string, resolved resolvedSources, err error) {
	servers = map[string]MCPServer{}
	resolved, err = m.resolveSources(doc.plugins, func(source SrcEntry) (string, error) {
		found, foundHooks, foundSkipped, key, err := m.pluginFeeds(source, doc.shape != "", doc.hookFormat != "")
		if err != nil {
			return "", err
		}
		for name, server := range found {
			servers[name] = server
		}
		hooks = append(hooks, foundHooks...)
		skipped = append(skipped, foundSkipped...)
		return key, nil
	})
	if err != nil {
		return nil, nil, nil, resolved, err
	}
	for name, server := range doc.servers {
		active, ok := m.activeServer(server)
		if !ok {
			delete(servers, name)
			continue
		}
		servers[name] = active
	}
	for _, hook := range doc.hooks {
		if m.scopedOff(hook.scope, hook.OnlyWhen) {
			continue
		}
		hooks = append(hooks, hook)
	}
	return servers, hooks, skipped, resolved, nil
}

// activeServer picks the server to write for a name: the declared one
// when its scope and condition match, else the one it overrides, and so
// on; ok is false when none applies.
func (m *materializer) activeServer(server MCPServer) (MCPServer, bool) {
	for current := &server; current != nil; current = current.fallback {
		if !m.scopedOff(current.scope, current.OnlyWhen) {
			return *current, true
		}
	}
	return MCPServer{}, false
}

// pluginFeeds reads the .mcp.json (in the Claude plugin shape,
// {"mcpServers": {...}}) and hooks/hooks.json of every plugin of a source.
func (m *materializer) pluginFeeds(source SrcEntry, wantServers bool, wantHooks bool) (servers map[string]MCPServer, hooks []Hook, skipped []string, key string, err error) {
	root, base, key, _, err := m.pluginRoot(source)
	if err != nil {
		return nil, nil, nil, "", err
	}
	plugins, _, err := root.manifest()
	if err != nil {
		return nil, nil, nil, "", err
	}
	servers = map[string]MCPServer{}
	for _, rel := range plugins {
		in := func(file string) string {
			if rel == "" {
				return file
			}
			return rel + "/" + file
		}
		label := "plugin " + path.Join(base, rel)
		if data, ok := root.readFile(in(".mcp.json")); ok && wantServers {
			var doc struct {
				Servers map[string]MCPServer `json:"mcpServers"`
			}
			if err := json.Unmarshal(data, &doc); err != nil {
				return nil, nil, nil, "", fmt.Errorf("%s: invalid .mcp.json: %s", label, err)
			}
			for name, server := range doc.Servers {
				servers[name] = server
			}
		}
		if data, ok := root.readFile(in("hooks/hooks.json")); ok && wantHooks {
			found, foundSkipped, err := parsePluginHooks(data, path.Join(agentsPluginsPath, base, rel), label)
			if err != nil {
				return nil, nil, nil, "", err
			}
			hooks = append(hooks, found...)
			skipped = append(skipped, foundSkipped...)
		}
	}
	return servers, hooks, skipped, key, nil
}

// desiredItems renders what the document should hold, in writing order:
// the version key first when the format has one, the servers by name,
// then the hooks in declaration order. skipped lists the hooks the
// harness cannot take.
func desiredItems(doc *managedDoc, servers map[string]MCPServer, hooks []Hook) (keys []string, items map[string]any, skipped []string) {
	items = map[string]any{}
	add := func(key string, value any) {
		if _, seen := items[key]; !seen {
			keys = append(keys, key)
		}
		items[key] = value
	}
	if doc.shape != "" {
		container := mcpContainer(doc.shape)
		for _, name := range sortedMCPNames(servers) {
			add(keyPath([]string{container, name}), renderMCPServer(doc.shape, servers[name]))
		}
	}
	if doc.hookFormat != "" {
		var elements []hookElement
		for _, hook := range hooks {
			rendered, reason := renderHook(doc.hookFormat, doc.harness, hook)
			if reason != "" {
				skipped = append(skipped, fmt.Sprintf("hook %s %q: %s", hook.Event, hook.Command, reason))
				continue
			}
			elements = append(elements, rendered...)
		}
		if len(elements) > 0 && hookFormats[doc.hookFormat].version {
			add("version", json.Number("1"))
		}
		for _, element := range elements {
			add(keyPath([]string{"hooks", element.event})+"#"+hookIdentity(element.value), element.value)
		}
	}
	return keys, items, skipped
}

// ensureManaged converges a managed document: jig's keys are upserted,
// keys that left the schema are removed, and every other byte of the
// document is the user's. Conflicts and modifications are kept and
// reported, never overwritten.
func (m *materializer) ensureManaged(entry Entry) error {
	doc := entry.File.managed
	expectedAbs := m.abs(entry.Path)
	record, hasState := m.state.Files[entry.Identity]
	if hasState {
		var err error
		if hasState, err = m.relocate(entry, record.Path); err != nil {
			return err
		}
		if hasState {
			record.Path = entry.Path
			m.state.Files[entry.Identity] = record
		}
	}
	servers, hooks, skipped, resolved, err := m.activeContent(doc)
	if err != nil {
		return err
	}
	// Codex takes a server only when its references have a Codex form;
	// the others leave the document and are reported.
	var codexTables map[string]string
	if doc.shape == "codex" {
		codexTables = map[string]string{}
		for _, name := range sortedMCPNames(servers) {
			rendered, err := renderCodexTable(name, servers[name])
			if err != nil {
				skipped = append(skipped, fmt.Sprintf("mcp server %s: %s", name, err))
				delete(servers, name)
				continue
			}
			codexTables[name] = rendered
		}
	}
	keys, desired, skippedHooks := desiredItems(doc, servers, hooks)
	skipped = append(skipped, skippedHooks...)
	report := func() {
		for _, line := range skipped {
			fmt.Fprintf(m.out, "  skipped %s\n", line)
		}
	}
	if len(desired) == 0 {
		err := m.ensureWithoutSources(entry, hasState)
		report()
		return err
	}
	if doc.shape == "codex" {
		err := m.ensureManagedTOML(entry, codexTables, resolved.note())
		report()
		return err
	}

	existed := pathExists(expectedAbs)
	root, created, err := loadManagedJSON(expectedAbs)
	if err != nil {
		return err
	}
	if hasState && record.Created {
		created = true
	}
	changed := false
	var counts managedCounts
	newKeys := map[string]string{}
	// Keys that left the schema go, unless the user changed them.
	for _, key := range sortedKeys(mapToBool(record.Keys)) {
		if _, wanted := desired[key]; wanted {
			continue
		}
		current, ok := getItem(root, key)
		if !ok {
			continue
		}
		if jsonHash(current) != record.Keys[key] {
			counts.abandoned++
			continue
		}
		removeItem(root, key)
		counts.removed++
		changed = true
	}
	for _, key := range keys {
		value := desired[key]
		hash := jsonHash(value)
		current, exists := getItem(root, key)
		recorded, tracked := record.Keys[key]
		switch {
		case exists && !tracked && jsonHash(current) != hash:
			counts.conflicts++
			continue
		case exists && tracked && jsonHash(current) != recorded && jsonHash(current) != hash:
			counts.modified++
			newKeys[key] = recorded
			continue
		case exists && jsonHash(current) == hash:
			newKeys[key] = hash
			counts.present++
			continue
		}
		if err := setItem(root, key, value); err != nil {
			return err
		}
		newKeys[key] = hash
		counts.written++
		changed = true
	}
	if changed || !pathExists(expectedAbs) {
		if err := writeManagedJSON(expectedAbs, root); err != nil {
			return err
		}
	}
	m.state.Files[entry.Identity] = StateFile{Path: entry.Path, Src: "managed", Managed: true, Created: created, Keys: newKeys}
	fmt.Fprintln(m.out, managedMessage(entry.Path, existed, changed, counts)+resolved.note())
	report()
	return nil
}

type managedCounts struct {
	written   int // keys written or updated
	present   int // keys already as wanted
	removed   int // keys removed because they left the schema
	conflicts int // keys the user already had with another value; kept
	modified  int // owned keys the user changed; kept
	abandoned int // keys that left the schema but were changed by the user; left
}

// managedMessage reports a managed document: "wrote" when jig created the
// document, "updated" when it changed keys inside an existing one.
func managedMessage(path string, existed bool, changed bool, counts managedCounts) string {
	verb := "present-file"
	if changed {
		verb = "wrote-file"
		if existed {
			verb = "updated-file"
		}
	}
	var parts []string
	add := func(n int, label string) {
		if n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, label))
		}
	}
	add(counts.written+counts.present, "managed keys")
	add(counts.removed, "removed")
	add(counts.conflicts, "conflicting kept")
	add(counts.modified, "modified kept")
	add(counts.abandoned, "left untracked")
	if len(parts) == 0 {
		return verb + ": " + path
	}
	return fmt.Sprintf("%s: %s (%s)", verb, path, strings.Join(parts, ", "))
}

// loadManagedJSON reads a document as an ordered object, or starts an
// empty one when the file does not exist (created reports which). A file
// that is not a JSON object is the user's and an error.
func loadManagedJSON(abs string) (*jsonObject, bool, error) {
	data, err := os.ReadFile(abs)
	if errors.Is(err, os.ErrNotExist) {
		return newJSONObject(), true, nil
	}
	if err != nil {
		return nil, false, err
	}
	if strings.TrimSpace(string(data)) == "" {
		return newJSONObject(), false, nil
	}
	value, err := decodeJSON(data)
	if err != nil {
		return nil, false, fmt.Errorf("not valid JSON (comments are not supported), left alone: %s", err)
	}
	root, ok := value.(*jsonObject)
	if !ok {
		return nil, false, errors.New("the document is not a JSON object, left alone")
	}
	return root, false, nil
}

func writeManagedJSON(abs string, root *jsonObject) error {
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return err
	}
	return os.WriteFile(abs, encodeJSON(root, "  "), 0o644)
}

// deleteManagedKeys takes jig's keys out of a managed document under the
// modification policy, deleting the document itself when jig created it
// and nothing is left. kept counts the modified keys left behind under
// abandonModified.
func deleteManagedKeys(root string, rel string, stateFile StateFile, policy modifiedPolicy) (kept int, err error) {
	abs := filepath.Join(root, rel)
	if !pathExists(abs) {
		return 0, nil
	}
	if strings.HasSuffix(rel, ".toml") {
		return deleteManagedTables(abs, stateFile, policy)
	}
	object, _, err := loadManagedJSON(abs)
	if err != nil {
		return 0, err
	}
	changed := false
	for _, key := range sortedKeys(mapToBool(stateFile.Keys)) {
		current, ok := getItem(object, key)
		if !ok {
			continue
		}
		if jsonHash(current) != stateFile.Keys[key] && policy != deleteModified {
			if policy == refuseModified {
				return 0, fmt.Errorf("locally modified: %s", key)
			}
			kept++
			continue
		}
		removeItem(object, key)
		changed = true
	}
	if object.empty() && stateFile.Created && kept == 0 {
		if err := os.Remove(abs); err != nil {
			return kept, err
		}
		pruneEmptyParents(root, filepath.Dir(rel))
		return kept, nil
	}
	if changed {
		return kept, writeManagedJSON(abs, object)
	}
	return kept, nil
}

// managedStatus summarizes a managed document for status: how many of
// jig's keys are missing or changed.
func managedStatus(abs string, stateFile StateFile) (missing int, modified int, err error) {
	if strings.HasSuffix(abs, ".toml") {
		return managedTableStatus(abs, stateFile)
	}
	object, _, err := loadManagedJSON(abs)
	if err != nil {
		return 0, 0, err
	}
	for key, hash := range stateFile.Keys {
		current, ok := getItem(object, key)
		switch {
		case !ok:
			missing++
		case jsonHash(current) != hash:
			modified++
		}
	}
	return missing, modified, nil
}
