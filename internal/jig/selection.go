package jig

import (
	"sort"
	"strings"
)

type NodeQuery struct {
	Path            string
	IncludeArchived bool
	Installed       InstalledNodes
}

type InstalledNodes struct {
	Repos map[string]bool
	Files map[string]bool
}

type NodeSelection struct {
	Path    string
	Entries []Entry
}

func normalizeQueryPath(path string) string {
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
		if entry.archived() && !query.IncludeArchived && !entryInstalled(model, entry, query.Installed) {
			continue
		}
		selection.Entries = append(selection.Entries, entry)
	}
	return selection, nil
}

func (ws *Workspace) Select(query NodeQuery) (NodeSelection, error) {
	query.Installed = ws.installedNodes()
	return ws.Model.Select(query)
}

func (ws *Workspace) installedNodes() InstalledNodes {
	return InstalledNodes{
		Repos: installedRepoIdentitySet(ws.Root, &ws.Model, &ws.State),
		Files: installedFileIdentitySet(ws.Root, &ws.Model, &ws.State),
	}
}

func (entry Entry) archived() bool {
	switch entry.Kind {
	case EntryRepo:
		return entry.Repo.Archived
	case EntryFile:
		return entry.File.Archived
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
