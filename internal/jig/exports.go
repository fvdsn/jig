package jig

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Repository exports lift a repository's own agent context into the
// agents block: `agents` on a $repo, or on a $group for every descendant,
// names a path inside the checkout per primitive, and that path joins the
// primitive's sources as a checkout source of the repository, read from
// the local checkout's working tree and active while the repository is
// installed. The checkout source is also available to $file and $dir
// directly, as {"repo": {...}, "path": ...}.

// exportableKeys are the agents block primitives a repository may export.
var exportableKeys = []string{"skills", "context", "subagents", "commands"}

func exportable(key string) bool {
	for _, known := range exportableKeys {
		if key == known {
			return true
		}
	}
	return false
}

// exportSources collects the checkout sources every repository exports
// for one primitive, in tree order.
func exportSources(model *Model, primitive string) SrcList {
	var sources SrcList
	for _, path := range sortedEntryPaths(model) {
		entry := model.Entries[path]
		if entry.Kind != EntryRepo {
			continue
		}
		if rel := entry.Repo.Agents[primitive]; rel != "" {
			sources = append(sources, SrcEntry{Repo: &Ref{ID: entry.Identity}, Path: rel, export: true})
		}
	}
	return sources
}

// hasExports reports whether any repository exports anything.
func hasExports(model *Model) bool {
	for _, entry := range model.Entries {
		if entry.Kind == EntryRepo && len(entry.Repo.Agents) > 0 {
			return true
		}
	}
	return false
}

// checkoutSource resolves a checkout source to the absolute path inside
// the repository's checkout and its state key. installed is false when
// the repository is not installed on this machine or the path is absent
// from its checkout, which gates the source off silently.
func (m *materializer) checkoutSource(source SrcEntry) (abs string, key string, installed bool) {
	entries := m.model.resolveRef(*source.Repo)
	if len(entries) != 1 || entries[0].Kind != EntryRepo {
		return "", "", false
	}
	repo := entries[0]
	if _, tracked := m.state.Repos[repo.Identity]; !tracked {
		return "", "", false
	}
	abs = filepath.Join(m.root, filepath.FromSlash(repo.Path), filepath.FromSlash(source.Path))
	key = "repo:" + repo.Identity + "#" + source.Path
	if !pathEntryExists(filepath.Join(m.root, filepath.FromSlash(repo.Path))) || !pathEntryExists(abs) {
		return abs, key, false
	}
	return abs, key, true
}

// gatedOff reports whether a source contributes nothing this run: its
// condition does not match, or it is a checkout source whose repository is
// not installed or lacks the path.
func (m *materializer) gatedOff(source SrcEntry) bool {
	if source.OnlyWhen != nil && !conditionMetIn(m.model, m.evidence, *source.OnlyWhen) {
		return true
	}
	if source.Repo != nil {
		_, _, installed := m.checkoutSource(source)
		return !installed
	}
	return false
}

// readCheckoutTree reads a checkout source as a dir source: a directory
// becomes a local tree, a single file a one-file source.
func readCheckoutTree(abs string) (dirSource, error) {
	info, err := os.Stat(abs)
	if err != nil {
		return dirSource{}, err
	}
	if !info.IsDir() {
		content, err := os.ReadFile(abs)
		if err != nil {
			return dirSource{}, err
		}
		return dirSource{name: filepath.Base(abs), file: content, tree: sha256Hex(content)}, nil
	}
	digest, err := localTreeDigest(abs)
	if err != nil {
		return dirSource{}, err
	}
	return dirSource{local: abs, tree: digest}, nil
}

// exportHeader frames an exported context section with the workspace
// path of the repository it came from, so an agent knows the section
// applies to that project.
func exportHeader(model *Model, source SrcEntry) []byte {
	entries := model.resolveRef(*source.Repo)
	if len(entries) != 1 {
		return nil
	}
	path := entries[0].Path
	return []byte(fmt.Sprintf("## %s\n\nExported by the repository at `%s`; it applies to that project.\n\n", path, path))
}

// validateCheckoutSource checks a checkout source: a single-target
// reference naming a repository, and a safe path inside it.
func validateCheckoutSource(result *validationResult, model *Model, owner string, source SrcEntry) {
	ref := *source.Repo
	switch {
	case ref.ID == "" && ref.Path == "" || len(ref.Tags) > 0 || (ref.ID != "" && ref.Path != ""):
		result.Errors = append(result.Errors, fmt.Sprintf("%s checkout source must reference one repository by id or exact path", owner))
	case strings.HasSuffix(ref.Path, "/*"):
		result.Errors = append(result.Errors, fmt.Sprintf("%s checkout source must reference one repository, not the subtree %s", owner, ref.Path))
	default:
		entries := model.resolveRef(ref)
		switch {
		case len(entries) == 0:
			result.Errors = append(result.Errors, fmt.Sprintf("%s checkout source repository %s does not name any entry", owner, ref.ID+ref.Path))
		case entries[0].Kind != EntryRepo:
			result.Errors = append(result.Errors, fmt.Sprintf("%s checkout source %s names a %s, not a repository", owner, ref.ID+ref.Path, entries[0].Kind))
		}
	}
	if source.Path == "" {
		result.Errors = append(result.Errors, fmt.Sprintf("%s checkout source needs a path inside the repository", owner))
	} else if err := validateSafePath(source.Path); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("%s checkout source has an invalid path %q: %s", owner, source.Path, err))
	}
}

// validateExports checks the agents field of repositories and groups:
// exportable keys with safe paths. Repositories carry inherited keys, so
// a group's mistake is reported once, by message.
func validateExports(result *validationResult, model *Model) {
	seen := map[string]bool{}
	report := func(msg string) {
		if !seen[msg] {
			seen[msg] = true
			result.Errors = append(result.Errors, msg)
		}
	}
	for _, path := range sortedEntryPaths(model) {
		entry := model.Entries[path]
		var agents map[string]string
		switch entry.Kind {
		case EntryRepo:
			agents = entry.Repo.Agents
		case EntryGroup:
			agents = entry.Group.Agents
		}
		for _, key := range sortedKeys(mapToBool(agents)) {
			if !exportable(key) {
				report(fmt.Sprintf("agents export %q is not exportable; use %s", key, strings.Join(exportableKeys, ", ")))
				continue
			}
			if err := validateSafePath(agents[key]); err != nil {
				report(fmt.Sprintf("agents export %s has an invalid path %q: %s", key, agents[key], err))
			}
		}
	}
}
