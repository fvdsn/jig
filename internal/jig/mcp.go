package jig

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
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
}

func (s MCPServer) remote() bool { return s.URL != "" }

var mcpServerName = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

// managedDoc makes a $file entry a managed document: a configuration file
// in which jig owns the keys it wrote and nothing else. It is derived per
// harness document from the mcp map (and later the hooks list) and from
// the plugins' .mcp.json files.
type managedDoc struct {
	harness string
	shape   string // how servers are rendered (see renderMCPServer)
	servers map[string]MCPServer
	plugins SrcList // plugin sources whose .mcp.json feeds the document
}

// mcpDocument is one harness's MCP configuration file.
type mcpDocument struct {
	path   string
	shape  string
	suffix string // identity suffix when a harness has several documents
}

// mcpDocuments is the mcp row of the matrix, per harness. Windsurf reads
// MCP servers from a global file only and is absent. Codex's TOML document
// is handled by the same entry with the toml shape.
var mcpDocuments = map[string][]mcpDocument{
	"claude":   {{path: ".mcp.json", shape: "claude"}},
	"codex":    {{path: ".codex/config.toml", shape: "codex"}},
	"copilot":  {{path: ".github/mcp.json", shape: "copilot"}, {path: ".vscode/mcp.json", shape: "vscode", suffix: "vscode"}},
	"cursor":   {{path: ".cursor/mcp.json", shape: "cursor"}},
	"opencode": {{path: "opencode.json", shape: "opencode"}},
	"gemini":   {{path: ".gemini/settings.json", shape: "gemini"}},
	"kiro":     {{path: ".kiro/settings/mcp.json", shape: "kiro"}},
}

const agentsMCPIdentity = "agents-mcp"

// expandMCP derives one managed document per harness document in use.
// There is no shared document, so nothing is canonical and nothing is
// derived when no harness is listed.
func expandMCP(block *AgentsBlock, harnesses []string, model *Model) error {
	if len(block.MCP) == 0 && len(block.Plugins) == 0 {
		return nil
	}
	for _, harness := range harnesses {
		for _, doc := range mcpDocuments[harness] {
			identity := agentsMCPIdentity + "-" + harness
			if doc.suffix != "" {
				identity += "-" + doc.suffix
			}
			entry := Entry{Path: doc.path, Identity: identity, Kind: EntryFile, File: &File{
				ID:          identity,
				Description: fmt.Sprintf("MCP servers from the agents block, as %s reads them", harness),
				managed:     &managedDoc{harness: harness, shape: doc.shape, servers: block.MCP, plugins: pluginSources(block.Plugins, "mcp")},
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

// Environment references: the neutral form is ${NAME}; VS Code and Cursor
// read ${env:NAME}. Every document gets its harness's form whichever one
// the schema used.
var envReference = regexp.MustCompile(`\$\{(?:env:)?([A-Za-z_][A-Za-z0-9_]*)\}`)

func translateEnvRefs(value string, vscodeStyle bool) string {
	return envReference.ReplaceAllStringFunc(value, func(match string) string {
		name := envReference.FindStringSubmatch(match)[1]
		if vscodeStyle {
			return "${env:" + name + "}"
		}
		return "${" + name + "}"
	})
}

func translateServer(server MCPServer, vscodeStyle bool) MCPServer {
	tr := func(s string) string { return translateEnvRefs(s, vscodeStyle) }
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
	server = translateServer(server, shape == "vscode" || shape == "cursor")
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

// keyPath renders a path for state and messages.
func keyPath(path []string) string { return strings.Join(path, "/") }

func splitKeyPath(key string) []string { return strings.Split(key, "/") }

// activeServers collects the document's servers: the plugins' .mcp.json
// servers first, then the block's, so an explicit declaration wins on a
// name; each gated by its condition.
func (m *materializer) activeServers(doc *managedDoc) (map[string]MCPServer, resolvedSources, error) {
	servers := map[string]MCPServer{}
	resolved, err := m.resolveSources(doc.plugins, func(source SrcEntry) (string, error) {
		found, key, err := m.pluginMCPServers(source)
		if err != nil {
			return "", err
		}
		for name, server := range found {
			servers[name] = server
		}
		return key, nil
	})
	if err != nil {
		return nil, resolved, err
	}
	for name, server := range doc.servers {
		if server.OnlyWhen != nil && !conditionMetIn(m.model, m.evidence, *server.OnlyWhen) {
			delete(servers, name)
			continue
		}
		servers[name] = server
	}
	return servers, resolved, nil
}

// pluginMCPServers reads the .mcp.json of every plugin of a source, in
// the Claude plugin shape ({"mcpServers": {...}}).
func (m *materializer) pluginMCPServers(source SrcEntry) (map[string]MCPServer, string, error) {
	root, _, key, _, err := m.pluginRoot(source)
	if err != nil {
		return nil, "", err
	}
	plugins, _, err := root.manifest()
	if err != nil {
		return nil, "", err
	}
	servers := map[string]MCPServer{}
	for _, rel := range plugins {
		file := ".mcp.json"
		if rel != "" {
			file = rel + "/.mcp.json"
		}
		data, ok := root.readFile(file)
		if !ok {
			continue
		}
		var doc struct {
			Servers map[string]MCPServer `json:"mcpServers"`
		}
		if err := json.Unmarshal(data, &doc); err != nil {
			return nil, "", fmt.Errorf("plugin %s: invalid .mcp.json: %s", rel, err)
		}
		for name, server := range doc.Servers {
			servers[name] = server
		}
	}
	return servers, key, nil
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
	servers, resolved, err := m.activeServers(doc)
	if err != nil {
		return err
	}
	if len(servers) == 0 {
		return m.ensureWithoutSources(entry, hasState)
	}
	if doc.shape == "codex" {
		return m.ensureManagedTOML(entry, servers, resolved.note())
	}

	desired := map[string]any{}
	container := mcpContainer(doc.shape)
	for _, name := range sortedMCPNames(servers) {
		desired[keyPath([]string{container, name})] = renderMCPServer(doc.shape, servers[name])
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
		current, ok := root.getPath(splitKeyPath(key))
		if !ok {
			continue
		}
		if jsonHash(current) != record.Keys[key] {
			counts.abandoned++
			continue
		}
		root.removePath(splitKeyPath(key))
		counts.removed++
		changed = true
	}
	for _, key := range sortedKeys(mapToBoolAny(desired)) {
		value := desired[key]
		hash := jsonHash(value)
		current, exists := root.getPath(splitKeyPath(key))
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
		if err := root.setPath(splitKeyPath(key), value); err != nil {
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

func mapToBoolAny(m map[string]any) map[string]bool {
	set := make(map[string]bool, len(m))
	for key := range m {
		set[key] = true
	}
	return set
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
		current, ok := object.getPath(splitKeyPath(key))
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
		object.removePath(splitKeyPath(key))
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
		current, ok := object.getPath(splitKeyPath(key))
		switch {
		case !ok:
			missing++
		case jsonHash(current) != hash:
			modified++
		}
	}
	return missing, modified, nil
}
