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
	Path   string
	Repos  []RepoEntry
	Files  []FileEntry
	Groups []GroupEntry
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
	for _, repoPath := range sortedRepoPaths(model) {
		entry := model.Repos[repoPath]
		if !nodePathMatches(path, repoPath) || entry.Repo.Archived && !query.IncludeArchived && !query.Installed.Repos[entry.Identity] {
			continue
		}
		selection.Repos = append(selection.Repos, entry)
	}
	for _, filePath := range sortedFilePaths(model) {
		entry := model.Files[filePath]
		if !nodePathMatches(path, filePath) || entry.File.Archived && !query.IncludeArchived && !query.Installed.Files[entry.Identity] {
			continue
		}
		selection.Files = append(selection.Files, entry)
	}
	for _, groupPath := range sortedGroupPaths(model) {
		entry := model.Groups[groupPath]
		if !nodePathMatches(path, groupPath) || entry.Group.Archived && !query.IncludeArchived && !groupInstalled(model, groupPath, query.Installed) {
			continue
		}
		selection.Groups = append(selection.Groups, entry)
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

func groupInstalled(model *Model, groupPath string, installed InstalledNodes) bool {
	for _, entry := range model.Repos {
		if installed.Repos[entry.Identity] && pathMatches(groupPath, entry.Path) {
			return true
		}
	}
	for _, entry := range model.Files {
		if installed.Files[entry.Identity] && pathMatches(groupPath, entry.Path) {
			return true
		}
	}
	return false
}

func nodePathMatches(queryPath string, entryPath string) bool {
	return queryPath == "" || pathMatches(queryPath, entryPath)
}

func (selection NodeSelection) repoPaths() []string {
	paths := make([]string, 0, len(selection.Repos))
	for _, entry := range selection.Repos {
		paths = append(paths, entry.Path)
	}
	return paths
}

func (selection NodeSelection) filePaths() []string {
	paths := make([]string, 0, len(selection.Files))
	for _, entry := range selection.Files {
		paths = append(paths, entry.Path)
	}
	return paths
}

func (selection NodeSelection) exactRepo() (RepoEntry, bool) {
	for _, entry := range selection.Repos {
		if entry.Path == selection.Path {
			return entry, true
		}
	}
	return RepoEntry{}, false
}

func (selection NodeSelection) exactFile() (FileEntry, bool) {
	for _, entry := range selection.Files {
		if entry.Path == selection.Path {
			return entry, true
		}
	}
	return FileEntry{}, false
}

func (selection NodeSelection) exactGroup() (GroupEntry, bool) {
	for _, entry := range selection.Groups {
		if entry.Path == selection.Path {
			return entry, true
		}
	}
	return GroupEntry{}, false
}

func pathMatches(path string, entryPath string) bool {
	return entryPath == path || strings.HasPrefix(entryPath, path+"/")
}

func sortedRepoPaths(model *Model) []string {
	paths := make([]string, 0, len(model.Repos))
	for path := range model.Repos {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func sortedFilePaths(model *Model) []string {
	paths := make([]string, 0, len(model.Files))
	for path := range model.Files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func sortedGroupPaths(model *Model) []string {
	paths := make([]string, 0, len(model.Groups))
	for path := range model.Groups {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func sortedKeys(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func repoIdentityToPath(model *Model) map[string]string {
	result := map[string]string{}
	for path, entry := range model.Repos {
		result[entry.Identity] = path
	}
	return result
}

func fileIdentityToPath(model *Model) map[string]string {
	result := map[string]string{}
	for path, entry := range model.Files {
		result[entry.Identity] = path
	}
	return result
}
