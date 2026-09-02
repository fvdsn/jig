package jig

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

type GraphOptions struct {
	Path            string
	IncludeArchived bool
}

// Graph prints the repository dependency graph as a Mermaid flowchart. The
// workspace tree is the visual skeleton: directories containing repositories
// render as nested subgraphs. Dependency edges onto group paths point at the
// subgraph itself, matching what the schema declares, and optional edges are
// dashed. The output is a raw diagram (no markdown fence) so it pipes into
// mermaid tooling as-is.
func Graph(options GraphOptions, out io.Writer) error {
	ws, err := loadWorkspace(false)
	if err != nil {
		return err
	}
	selection, err := ws.Select(NodeQuery{Path: options.Path, IncludeArchived: options.IncludeArchived})
	if err != nil {
		return err
	}
	selected := selection.repoPaths()
	if len(selected) == 0 {
		return noEntriesMatchError(&ws.Model, "repositories", selection.Path, nil)
	}

	// Nodes are the selected repositories plus any edge targets outside the
	// selection, so no arrow dangles. Only selected repositories contribute
	// edges; external nodes are context.
	type edge struct {
		from     string
		to       string
		optional bool
	}
	nodes := map[string]bool{}
	for _, path := range selected {
		nodes[path] = true
	}
	edges := map[string]edge{}
	groupTargets := map[string]bool{}
	for _, path := range selected {
		entry := ws.Model.Entries[path]
		for _, dep := range entry.Repo.DependsOn {
			// Single-repo refs become repository nodes; subtree and tag
			// refs are drawn as one collective target.
			var to string
			switch {
			case dep.ID != "":
				matches := ws.Model.resolveRepoRef(dep.Ref)
				if len(matches) == 0 {
					continue
				}
				to = matches[0].Path
				nodes[to] = true
			case dep.Path != "":
				if base, subtree := subtreePath(dep.Path); subtree {
					if base == "" {
						base = "*"
					}
					to = base
					groupTargets[base] = true
				} else {
					to = dep.Path
					nodes[to] = true
				}
			default:
				to = "tags: " + strings.Join(dep.Tags, ",")
				groupTargets[to] = true
			}
			key := path + "\x00" + to
			if prev, ok := edges[key]; ok && !prev.optional {
				continue
			}
			edges[key] = edge{from: path, to: to, optional: dep.Optional}
		}
	}

	// A group target whose repositories are drawn is addressed as the
	// subgraph enclosing them; otherwise it becomes a plain node.
	isSubgraph := func(dir string) bool {
		for node := range nodes {
			if strings.HasPrefix(node, dir+"/") {
				return true
			}
		}
		return false
	}

	root := newGraphDir()
	for node := range nodes {
		root.insert(node)
	}
	fmt.Fprintln(out, "flowchart TD")
	root.render(out, "  ", "")
	for _, group := range sortedKeys(groupTargets) {
		if !isSubgraph(group) {
			fmt.Fprintf(out, "  %s[\"%s\"]\n", mermaidID(group), group)
		}
	}
	keys := make([]string, 0, len(edges))
	for key := range edges {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		arrow := "-->"
		if edges[key].optional {
			arrow = "-.->"
		}
		fmt.Fprintf(out, "  %s %s %s\n", mermaidID(edges[key].from), arrow, mermaidID(edges[key].to))
	}
	return nil
}

// graphDir is the drawn workspace tree: directories become subgraphs and
// repositories become leaf nodes labeled with their last path segment.
type graphDir struct {
	dirs  map[string]*graphDir
	repos []string // full repo paths directly at this level
}

func newGraphDir() *graphDir {
	return &graphDir{dirs: map[string]*graphDir{}}
}

func (d *graphDir) insert(repoPath string) {
	node := d
	segments := strings.Split(repoPath, "/")
	for _, segment := range segments[:len(segments)-1] {
		next, ok := node.dirs[segment]
		if !ok {
			next = newGraphDir()
			node.dirs[segment] = next
		}
		node = next
	}
	node.repos = append(node.repos, repoPath)
}

func (d *graphDir) render(out io.Writer, indent string, prefix string) {
	names := make([]string, 0, len(d.dirs))
	for name := range d.dirs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		path := name
		if prefix != "" {
			path = prefix + "/" + name
		}
		fmt.Fprintf(out, "%ssubgraph %s [\"%s\"]\n", indent, mermaidID(path), name)
		d.dirs[name].render(out, indent+"  ", path)
		fmt.Fprintf(out, "%send\n", indent)
	}
	sort.Strings(d.repos)
	for _, repoPath := range d.repos {
		segments := strings.Split(repoPath, "/")
		fmt.Fprintf(out, "%s%s[\"%s\"]\n", indent, mermaidID(repoPath), segments[len(segments)-1])
	}
}

// mermaidID turns a workspace path into a safe mermaid identifier. Flowchart
// keywords get a trailing underscore so a path segment like "end" cannot
// break the diagram.
func mermaidID(path string) string {
	var b strings.Builder
	for _, r := range path {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	id := b.String()
	switch id {
	case "end", "subgraph", "graph", "flowchart", "direction", "style", "classDef", "class", "click", "linkStyle":
		id += "_"
	}
	return id
}
