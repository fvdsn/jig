package jig

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Codex keeps its MCP servers in config.toml, a file people edit by hand
// with comments, so jig edits it table by table: it owns the whole
// [mcp_servers.<name>] tables it wrote, replacing or removing those lines
// and leaving every other line as it is. No TOML parser is needed: a
// table runs from its header to the next header or the end of the file.

const codexTablePrefix = "mcp_servers."

// renderCodexTable renders one server as a TOML table.
func renderCodexTable(name string, server MCPServer) string {
	server = translateServer(server, false)
	var b strings.Builder
	fmt.Fprintf(&b, "[%s%s]\n", codexTablePrefix, tomlKey(name))
	if server.remote() {
		fmt.Fprintf(&b, "url = %s\n", tomlString(server.URL))
		if len(server.Headers) > 0 {
			b.WriteString(tomlInlineTable("headers", server.Headers))
		}
	} else {
		fmt.Fprintf(&b, "command = %s\n", tomlString(server.Command))
		if len(server.Args) > 0 {
			quoted := make([]string, len(server.Args))
			for i, arg := range server.Args {
				quoted[i] = tomlString(arg)
			}
			fmt.Fprintf(&b, "args = [%s]\n", strings.Join(quoted, ", "))
		}
		if len(server.Env) > 0 {
			b.WriteString(tomlInlineTable("env", server.Env))
		}
	}
	return b.String()
}

func tomlKey(name string) string {
	for _, r := range name {
		bare := r == '_' || r == '-' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if !bare {
			return tomlString(name)
		}
	}
	return name
}

func tomlInlineTable(key string, values map[string]string) string {
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = tomlKey(k) + " = " + tomlString(values[k])
	}
	return key + " = { " + strings.Join(parts, ", ") + " }\n"
}

// tomlTables splits a document into its preamble and tables, each table
// keyed by its header name and holding its lines verbatim.
type tomlTable struct {
	name  string // "" for the preamble
	lines []string
}

func splitTOMLTables(data string) []tomlTable {
	var tables []tomlTable
	current := tomlTable{}
	for _, line := range strings.SplitAfter(data, "\n") {
		if line == "" {
			continue
		}
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && !strings.HasPrefix(trimmed, "[[") && strings.HasSuffix(trimmed, "]") {
			tables = append(tables, current)
			name := strings.TrimSpace(trimmed[1 : len(trimmed)-1])
			current = tomlTable{name: strings.Trim(name, `"`)}
		}
		current.lines = append(current.lines, line)
	}
	tables = append(tables, current)
	return tables
}

func joinTOMLTables(tables []tomlTable) string {
	var b strings.Builder
	for _, table := range tables {
		for _, line := range table.lines {
			b.WriteString(line)
		}
	}
	return b.String()
}

// tableHash fingerprints a table's content without its trailing blank
// lines, so spacing edits do not count as modifications.
func tableHash(lines []string) string {
	return sha256Hex([]byte(strings.TrimRight(strings.Join(lines, ""), "\n") + "\n"))
}

// ensureManagedTOML is ensureManaged for the Codex document.
func (m *materializer) ensureManagedTOML(entry Entry, servers map[string]MCPServer, note string) error {
	expectedAbs := m.abs(entry.Path)
	record, hasState := m.state.Files[entry.Identity]
	data, err := os.ReadFile(expectedAbs)
	existed := err == nil
	created := os.IsNotExist(err)
	if err != nil && !created {
		return err
	}
	if hasState && record.Created {
		created = true
	}
	tables := splitTOMLTables(string(data))
	index := func(name string) int {
		for i, table := range tables {
			if table.name == codexTablePrefix+name {
				return i
			}
		}
		return -1
	}
	changed := false
	var counts managedCounts
	newKeys := map[string]string{}
	for _, key := range sortedKeys(mapToBool(record.Keys)) {
		if _, wanted := servers[key]; wanted {
			continue
		}
		i := index(key)
		if i < 0 {
			continue
		}
		if tableHash(tables[i].lines) != record.Keys[key] {
			counts.abandoned++
			continue
		}
		tables = append(tables[:i], tables[i+1:]...)
		counts.removed++
		changed = true
	}
	for _, name := range sortedMCPNames(servers) {
		rendered := renderCodexTable(name, servers[name])
		lines := strings.SplitAfter(rendered, "\n")
		lines = lines[:len(lines)-1]
		hash := tableHash(lines)
		i := index(name)
		recorded, tracked := record.Keys[name]
		switch {
		case i >= 0 && !tracked && tableHash(tables[i].lines) != hash:
			counts.conflicts++
			continue
		case i >= 0 && tracked && tableHash(tables[i].lines) != recorded && tableHash(tables[i].lines) != hash:
			counts.modified++
			newKeys[name] = recorded
			continue
		case i >= 0 && tableHash(tables[i].lines) == hash:
			newKeys[name] = hash
			counts.present++
			continue
		case i >= 0:
			tables[i].lines = append(lines, "\n")
		default:
			if last := len(tables) - 1; last >= 0 && len(tables[last].lines) > 0 && !strings.HasSuffix(tables[last].lines[len(tables[last].lines)-1], "\n\n") {
				if strings.TrimSpace(tables[last].lines[len(tables[last].lines)-1]) != "" {
					tables[last].lines = append(tables[last].lines, "\n")
				}
			}
			tables = append(tables, tomlTable{name: codexTablePrefix + name, lines: append(lines, "\n")})
		}
		newKeys[name] = hash
		counts.written++
		changed = true
	}
	if changed || created {
		if err := os.MkdirAll(filepath.Dir(expectedAbs), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(expectedAbs, []byte(strings.TrimRight(joinTOMLTables(tables), "\n")+"\n"), 0o644); err != nil {
			return err
		}
	}
	m.state.Files[entry.Identity] = StateFile{Path: entry.Path, Src: "managed", Managed: true, Created: created, Keys: newKeys}
	fmt.Fprintln(m.out, managedMessage(entry.Path, existed, changed, counts)+note)
	return nil
}

// deleteManagedTables is deleteManagedKeys for the Codex document.
func deleteManagedTables(abs string, stateFile StateFile, policy modifiedPolicy) (kept int, err error) {
	data, err := os.ReadFile(abs)
	if err != nil {
		return 0, err
	}
	tables := splitTOMLTables(string(data))
	changed := false
	for _, name := range sortedKeys(mapToBool(stateFile.Keys)) {
		for i := 0; i < len(tables); i++ {
			if tables[i].name != codexTablePrefix+name {
				continue
			}
			if tableHash(tables[i].lines) != stateFile.Keys[name] && policy != deleteModified {
				if policy == refuseModified {
					return 0, fmt.Errorf("locally modified: %s", name)
				}
				kept++
				break
			}
			tables = append(tables[:i], tables[i+1:]...)
			changed = true
			break
		}
	}
	rest := strings.TrimSpace(joinTOMLTables(tables))
	if rest == "" && stateFile.Created && kept == 0 {
		return kept, os.Remove(abs)
	}
	if changed {
		return kept, os.WriteFile(abs, []byte(rest+"\n"), 0o644)
	}
	return kept, nil
}

func managedTableStatus(abs string, stateFile StateFile) (missing int, modified int, err error) {
	data, err := os.ReadFile(abs)
	if err != nil {
		return 0, 0, err
	}
	tables := splitTOMLTables(string(data))
	for name, hash := range stateFile.Keys {
		found := false
		for _, table := range tables {
			if table.name == codexTablePrefix+name {
				found = true
				if tableHash(table.lines) != hash {
					modified++
				}
				break
			}
		}
		if !found {
			missing++
		}
	}
	return missing, modified, nil
}
