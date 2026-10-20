package jig

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// A plugin is a directory in the Claude Code plugin layout (a manifest
// under .claude-plugin/, .plugin/, or .cursor-plugin/, and beside it
// skills/, agents/, commands/, and whatever the plugin keeps for itself);
// a marketplace is a directory whose manifest lists plugins by relative
// path. Plugins are kept whole in the store at .agents/plugins, and their
// skills, agents, and commands feed the matching primitives with the
// links out of each unit rewritten into the store.

var pluginManifestDirs = []string{".claude-plugin", ".plugin", ".cursor-plugin"}

const (
	agentsPluginsPath     = ".agents/plugins"
	agentsPluginsIdentity = "agents-plugins"
)

// pluginSubdirs maps a primitive to the plugin directory that feeds it.
var pluginSubdirs = map[string]string{"skills": "skills", "subagents": "agents", "commands": "commands"}

// pluginSources turns the block's plugin list into sources of one
// primitive: the same entries, marked with the subdirectory to take.
func pluginSources(plugins SrcList, subdir string) SrcList {
	if subdir == "" {
		return nil
	}
	sources := make(SrcList, 0, len(plugins))
	for _, source := range plugins {
		source.plugin = subdir
		sources = append(sources, source)
	}
	return sources
}

// pluginTree abstracts a resolved plugin or marketplace source: a git
// tree in a mirror, or a local directory.
type pluginTree struct {
	mirror string // git: the mirror; empty for local
	tree   string // git: the tree id of the source
	local  string // local: absolute path of the source
}

// readFile reads one file of the source tree, relative to its root.
func (t pluginTree) readFile(rel string) ([]byte, bool) {
	if t.local != "" {
		data, err := os.ReadFile(filepath.Join(t.local, filepath.FromSlash(rel)))
		return data, err == nil
	}
	out, err := git(t.mirror, "cat-file", "-p", t.tree+":"+rel)
	if err != nil {
		return nil, false
	}
	return []byte(out), true
}

// subtree reports whether rel is a directory of the source and returns
// its tree id (git) or absolute path (local) as a child pluginTree.
func (t pluginTree) subtree(rel string) (pluginTree, bool) {
	if rel == "" {
		return t, true
	}
	if t.local != "" {
		abs := filepath.Join(t.local, filepath.FromSlash(rel))
		if info, err := os.Stat(abs); err == nil && info.IsDir() {
			return pluginTree{local: abs}, true
		}
		return pluginTree{}, false
	}
	out, err := git(t.mirror, "rev-parse", t.tree+":"+rel)
	if err != nil {
		return pluginTree{}, false
	}
	oid := strings.TrimSpace(out)
	if objType, err := git(t.mirror, "cat-file", "-t", oid); err != nil || strings.TrimSpace(objType) != "tree" {
		return pluginTree{}, false
	}
	return pluginTree{mirror: t.mirror, tree: oid}, true
}

// files lists every regular file under the source, relative to its root.
func (t pluginTree) files() (map[string]bool, error) {
	files := map[string]bool{}
	if t.local != "" {
		err := filepath.WalkDir(t.local, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.Type().IsRegular() {
				rel, _ := filepath.Rel(t.local, p)
				files[filepath.ToSlash(rel)] = true
			}
			return nil
		})
		return files, err
	}
	out, err := git(t.mirror, "ls-tree", "-r", "--name-only", t.tree)
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(out, "\n") {
		if line != "" {
			files[line] = true
		}
	}
	return files, nil
}

// manifest finds the plugin or marketplace manifest of the source: the
// relative paths of the plugins it holds ("" for the source itself when
// it is a plugin), plus the names of marketplace entries skipped because
// their source is not a relative path.
func (t pluginTree) manifest() (plugins []string, skipped []string, err error) {
	for _, dir := range pluginManifestDirs {
		if data, ok := t.readFile(dir + "/marketplace.json"); ok {
			return parseMarketplace(data)
		}
	}
	for _, dir := range pluginManifestDirs {
		if _, ok := t.readFile(dir + "/plugin.json"); ok {
			return []string{""}, nil, nil
		}
	}
	return nil, nil, fmt.Errorf("no plugin.json or marketplace.json under %s", strings.Join(pluginManifestDirs, ", "))
}

// parseMarketplace reads the plugins a marketplace lists by relative path.
func parseMarketplace(data []byte) ([]string, []string, error) {
	var manifest struct {
		Plugins []struct {
			Name   string          `json:"name"`
			Source json.RawMessage `json:"source"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, nil, fmt.Errorf("invalid marketplace.json: %s", err)
	}
	var plugins, skipped []string
	for _, entry := range manifest.Plugins {
		var source string
		if err := json.Unmarshal(entry.Source, &source); err != nil || source == "" {
			skipped = append(skipped, entry.Name)
			continue
		}
		rel := path.Clean(strings.TrimPrefix(source, "./"))
		if strings.HasPrefix(source, "/") || strings.Contains(source, ":") || rel == "." || strings.HasPrefix(rel, "../") {
			skipped = append(skipped, entry.Name)
			continue
		}
		plugins = append(plugins, rel)
	}
	sort.Strings(plugins)
	return plugins, skipped, nil
}

// pluginRewrite is the context the link rewriter needs for one plugin's
// files: where the plugin lives in the store, what files it has, and
// which of its directories is being written.
type pluginRewrite struct {
	storePath string          // workspace path of the plugin in the store
	files     map[string]bool // plugin files, relative to the plugin root
	subdir    string          // skills, agents, or commands
}

var markdownLink = regexp.MustCompile(`\]\(([^)\s]+)\)`)
var urlScheme = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*:`)

// rewriteLinks rewrites the relative markdown links of a file written out
// of a plugin that point inside the plugin but outside the written unit
// (the skill directory for a skill, the file itself otherwise) into
// relative paths to the plugin's copy in the store, from writtenDir, the
// workspace directory the file lands in. Links with a scheme, root-absolute
// links, bare fragments, links whose target does not exist in the plugin,
// and links that escape it are left alone; fragments and queries survive.
func (rw *pluginRewrite) rewriteLinks(rel string, content []byte, writtenDir string) []byte {
	if _, _, ok := markdownStem(rel); !ok {
		return content
	}
	inPlugin := rw.subdir + "/" + rel
	unit := inPlugin
	if rw.subdir == "skills" {
		if parts := strings.SplitN(rel, "/", 2); len(parts) == 2 {
			unit = rw.subdir + "/" + parts[0] + "/"
		}
	}
	return markdownLink.ReplaceAllFunc(content, func(match []byte) []byte {
		target := string(match[2 : len(match)-1])
		suffix := ""
		if i := strings.IndexAny(target, "#?"); i >= 0 {
			target, suffix = target[:i], target[i:]
		}
		if target == "" || urlScheme.MatchString(target) || strings.HasPrefix(target, "/") {
			return match
		}
		resolved := path.Clean(path.Join(path.Dir(inPlugin), target))
		if resolved == ".." || strings.HasPrefix(resolved, "../") || !rw.files[resolved] {
			return match
		}
		if resolved == inPlugin || (strings.HasSuffix(unit, "/") && strings.HasPrefix(resolved, unit)) {
			return match
		}
		newTarget, err := filepath.Rel(filepath.FromSlash(writtenDir), filepath.FromSlash(rw.storePath+"/"+resolved))
		if err != nil {
			return match
		}
		return []byte("](" + filepath.ToSlash(newTarget) + suffix + ")")
	})
}
