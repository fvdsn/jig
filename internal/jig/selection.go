package jig

import (
	"fmt"
	"sort"
	"strings"
)

type NodeQuery struct {
	Path            string
	Id              string // when set, selects the entry with this identity instead of a path
	IncludeArchived bool
	Tags            []string   // when set, only entries carrying all of these tags match
	Meta            MetaFilter // when set, only entries carrying the meta key (and value) match
	Installed       InstalledNodes
}

// MetaFilter selects entries by user-defined meta: a bare key matches
// entries carrying it, and with HasValue the value must also be equal.
// The zero value matches everything.
type MetaFilter struct {
	Key      string
	Value    string
	HasValue bool
}

type InstalledNodes struct {
	Repos map[string]bool
	Files map[string]bool
	Dirs  map[string]bool
}

type NodeSelection struct {
	Path    string
	Entries []Entry
}

// normalizeQueryPath drops a trailing "/" and the explicit subtree marker
// "/*"; CLI paths select the node and its subtree either way.
func normalizeQueryPath(path string) string {
	if path == "*" {
		return ""
	}
	path = strings.TrimSuffix(path, "/*")
	return strings.TrimRight(path, "/")
}

func (model *Model) Select(query NodeQuery) (NodeSelection, error) {
	path := query.Path
	if path != "" {
		path = normalizeQueryPath(path)
		if err := validateSafePath(path); err != nil {
			return NodeSelection{}, err
		}
	}

	selection := NodeSelection{Path: path}
	for _, entryPath := range sortedEntryPaths(model) {
		entry := model.Entries[entryPath]
		if !nodePathMatches(path, entryPath) {
			continue
		}
		if !entry.hasAllTags(query.Tags) {
			continue
		}
		if !entry.matchesMeta(query.Meta) {
			continue
		}
		if entry.archived() && !query.IncludeArchived && !entryInstalled(model, entry, query.Installed) {
			continue
		}
		selection.Entries = append(selection.Entries, entry)
	}
	return selection, nil
}

// describeQuery renders a path-and-tags query for error messages. An empty
// path with tags means the whole workspace, so only the tags are shown.
func describeQuery(path string, tags []string) string {
	if len(tags) == 0 {
		return fmt.Sprintf("%q", path)
	}
	if path == "" {
		return "tags " + strings.Join(tags, ",")
	}
	return fmt.Sprintf("%q with tags %s", path, strings.Join(tags, ","))
}

// noEntriesMatchError explains an empty selection. A selector that cannot
// match anything (a typo'd tag or path) is the real mistake and is reported
// directly; otherwise the query is described as matching nothing.
func noEntriesMatchError(model *Model, what string, path string, tags []string) error {
	if err := unknownSelectorError(model, path, tags); err != nil {
		return err
	}
	return fmt.Errorf("no %s match %s", what, describeQuery(path, tags))
}

// unknownSelectorError reports a query selector that cannot match anything:
// a tag no schema entry carries, or a path where no entry is defined —
// usually typos, so the closest existing tag or path is suggested when one
// is near. It returns nil when every selector is real and the query simply
// selected nothing.
func unknownSelectorError(model *Model, path string, tags []string) error {
	known := schemaTags(model)
	for _, tag := range tags {
		if known[tag] {
			continue
		}
		if suggestion := closestMatch(sortedKeys(known), tag); suggestion != "" {
			return fmt.Errorf("no entries carry tag %q; did you mean %q?", tag, suggestion)
		}
		return fmt.Errorf("no entries carry tag %q; jig tags lists the tags in use", tag)
	}
	if path != "" && !pathDefined(model, path) {
		if suggestion := closestMatch(sortedEntryPaths(model), path); suggestion != "" {
			return fmt.Errorf("no entry is defined at %q; did you mean %q?", path, suggestion)
		}
		return fmt.Errorf("no entry is defined at %q", path)
	}
	return nil
}

// pathDefined reports whether path names a declared entry or a subtree
// containing one.
func pathDefined(model *Model, path string) bool {
	for entryPath := range model.Entries {
		if pathMatches(path, entryPath) {
			return true
		}
	}
	return false
}

// schemaTags returns every tag carried by any entry, inherited tags included.
func schemaTags(model *Model) map[string]bool {
	tags := map[string]bool{}
	for _, entry := range model.Entries {
		for _, tag := range entry.Tags {
			tags[tag] = true
		}
	}
	return tags
}

// closestMatch returns the candidate nearest to the given value, when it is
// close enough to be a plausible typo; ties go to the first candidate, so
// pass candidates sorted for deterministic suggestions.
func closestMatch(candidates []string, value string) string {
	best, bestDistance := "", 3
	for _, candidate := range candidates {
		if d := editDistance(value, candidate); d < bestDistance {
			best, bestDistance = candidate, d
		}
	}
	return best
}

func editDistance(a, b string) int {
	previous := make([]int, len(b)+1)
	current := make([]int, len(b)+1)
	for j := range previous {
		previous[j] = j
	}
	for i := 1; i <= len(a); i++ {
		current[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			current[j] = min(previous[j]+1, min(current[j-1]+1, previous[j-1]+cost))
		}
		previous, current = current, previous
	}
	return previous[len(b)]
}

func (entry Entry) matchesMeta(filter MetaFilter) bool {
	if filter.Key == "" {
		return true
	}
	value, ok := entry.Meta[filter.Key]
	if !ok {
		return false
	}
	return !filter.HasValue || value == filter.Value
}

func (entry Entry) hasAllTags(tags []string) bool {
	for _, tag := range tags {
		found := false
		for _, entryTag := range entry.Tags {
			if entryTag == tag {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func (ws *Workspace) Select(query NodeQuery) (NodeSelection, error) {
	// An id resolves to its entry's path up front and then behaves as an
	// exact query from the workspace root: explicit ids are not scoped by
	// the current directory and include archived entries.
	if query.Id != "" {
		entry, ok := ws.Model.entryByIdentity(query.Id)
		if !ok {
			return NodeSelection{}, fmt.Errorf("no entry has id %q", query.Id)
		}
		query.Path = entry.Path
		query.Id = ""
		query.IncludeArchived = true
		query.Installed = ws.installedNodes()
		return ws.Model.Select(query)
	}
	resolved, err := ws.ResolvePath(normalizeQueryPath(query.Path))
	if err != nil {
		return NodeSelection{}, err
	}
	query.Path = resolved
	query.Installed = ws.installedNodes()
	return ws.Model.Select(query)
}

// entryByIdentity returns the entry whose identity matches, of any kind.
func (model *Model) entryByIdentity(identity string) (Entry, bool) {
	for _, path := range sortedEntryPaths(model) {
		if entry := model.Entries[path]; entry.Identity == identity {
			return entry, true
		}
	}
	return Entry{}, false
}

func (ws *Workspace) installedNodes() InstalledNodes {
	return InstalledNodes{
		Repos: installedRepoIdentitySet(ws.Root, &ws.Model, &ws.State),
		Files: installedFileIdentitySet(ws.Root, &ws.Model, &ws.State),
		Dirs:  installedDirIdentitySet(ws.Root, &ws.Model, &ws.State),
	}
}

func (entry Entry) archived() bool {
	switch entry.Kind {
	case EntryRepo:
		return entry.Repo.Archived
	case EntryFile:
		return entry.File.Archived
	case EntryDir:
		return entry.Dir.Archived
	case EntryGroup:
		return entry.Group.Archived
	default:
		return false
	}
}

func (entry Entry) description() string {
	switch entry.Kind {
	case EntryRepo:
		return entry.Repo.Description
	case EntryFile:
		return entry.File.Description
	case EntryDir:
		return entry.Dir.Description
	case EntryGroup:
		return entry.Group.Description
	default:
		return ""
	}
}

func (entry Entry) dependsOn() []Dependency {
	switch entry.Kind {
	case EntryRepo:
		return entry.Repo.DependsOn
	case EntryGroup:
		return entry.Group.DependsOn
	default:
		return nil
	}
}

func entryInstalled(model *Model, entry Entry, installed InstalledNodes) bool {
	switch entry.Kind {
	case EntryRepo:
		return installed.Repos[entry.Identity]
	case EntryFile:
		return installed.Files[entry.Identity]
	case EntryDir:
		return installed.Dirs[entry.Identity]
	case EntryGroup:
		return groupInstalled(model, entry.Path, installed)
	default:
		return false
	}
}

func groupInstalled(model *Model, groupPath string, installed InstalledNodes) bool {
	for _, entry := range model.Entries {
		switch entry.Kind {
		case EntryRepo:
			if installed.Repos[entry.Identity] && pathMatches(groupPath, entry.Path) {
				return true
			}
		case EntryFile:
			if installed.Files[entry.Identity] && pathMatches(groupPath, entry.Path) {
				return true
			}
		case EntryDir:
			if installed.Dirs[entry.Identity] && pathMatches(groupPath, entry.Path) {
				return true
			}
		}
	}
	return false
}

func nodePathMatches(queryPath string, entryPath string) bool {
	return queryPath == "" || pathMatches(queryPath, entryPath)
}

func (selection NodeSelection) ofKind(kind EntryKind) []Entry {
	entries := make([]Entry, 0, len(selection.Entries))
	for _, entry := range selection.Entries {
		if entry.Kind == kind {
			entries = append(entries, entry)
		}
	}
	return entries
}

func (selection NodeSelection) repoPaths() []string {
	return entryPaths(selection.ofKind(EntryRepo))
}

func (selection NodeSelection) filePaths() []string {
	return entryPaths(selection.ofKind(EntryFile))
}

func entryPaths(entries []Entry) []string {
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		paths = append(paths, entry.Path)
	}
	return paths
}

func (selection NodeSelection) exact(kind EntryKind) (Entry, bool) {
	for _, entry := range selection.Entries {
		if entry.Path == selection.Path && entry.Kind == kind {
			return entry, true
		}
	}
	return Entry{}, false
}

func (selection NodeSelection) exactRepo() (Entry, bool) {
	return selection.exact(EntryRepo)
}

func (selection NodeSelection) exactFile() (Entry, bool) {
	return selection.exact(EntryFile)
}

func (selection NodeSelection) exactGroup() (Entry, bool) {
	return selection.exact(EntryGroup)
}

func (model *Model) entry(path string, kind EntryKind) (Entry, bool) {
	entry, ok := model.Entries[path]
	return entry, ok && entry.Kind == kind
}

func pathMatches(path string, entryPath string) bool {
	return entryPath == path || strings.HasPrefix(entryPath, path+"/")
}

func sortedEntryPaths(model *Model) []string {
	paths := make([]string, 0, len(model.Entries))
	for path := range model.Entries {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func sortedPathsOfKind(model *Model, kind EntryKind) []string {
	var paths []string
	for _, path := range sortedEntryPaths(model) {
		if model.Entries[path].Kind == kind {
			paths = append(paths, path)
		}
	}
	return paths
}

func sortedRepoPaths(model *Model) []string {
	return sortedPathsOfKind(model, EntryRepo)
}

func sortedFilePaths(model *Model) []string {
	return sortedPathsOfKind(model, EntryFile)
}

func sortedKeys(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedMetaKeys(meta map[string]string) []string {
	keys := make([]string, 0, len(meta))
	for key := range meta {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func identityToPath(model *Model, kind EntryKind) map[string]string {
	result := map[string]string{}
	for path, entry := range model.Entries {
		if entry.Kind == kind {
			result[entry.Identity] = path
		}
	}
	return result
}

func repoIdentityToPath(model *Model) map[string]string {
	return identityToPath(model, EntryRepo)
}

func fileIdentityToPath(model *Model) map[string]string {
	return identityToPath(model, EntryFile)
}
