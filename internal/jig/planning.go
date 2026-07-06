package jig

import (
	"fmt"
	"strings"
)

type planOptions struct {
	IncludeOptional          bool
	IncludeInstalledOptional bool
	IncludeArchived          bool
	IncludeRoots             bool
	Installed                map[string]bool
	InstalledFiles           map[string]bool
	InstalledDirs            map[string]bool
}

type plan struct {
	Repos []string
	Files []string
	Dirs  []string
}

func resolvePlan(model *Model, roots []string, opts planOptions) (plan, error) {
	if opts.Installed == nil {
		opts.Installed = map[string]bool{}
	}
	if opts.InstalledFiles == nil {
		opts.InstalledFiles = map[string]bool{}
	}
	active := map[string]bool{}
	rootIDs := map[string]bool{}
	for _, root := range roots {
		entry, ok := model.entry(root, EntryRepo)
		if !ok {
			return plan{}, fmt.Errorf("unknown repository %q", root)
		}
		rootIDs[entry.Identity] = true
		if opts.IncludeRoots && !archivedExcluded(entry, opts.Installed, opts.IncludeArchived) {
			active[root] = true
		}
	}

	changed := true
	for changed {
		changed = false
		for _, root := range roots {
			if !opts.IncludeRoots {
				if changedRoot, err := addDependencies(model, root, active, opts, rootIDs); err != nil {
					return plan{}, err
				} else if changedRoot {
					changed = true
				}
			}
		}
		for _, repoPath := range sortedRepoPaths(model) {
			entry, _ := model.entry(repoPath, EntryRepo)
			if archivedExcluded(entry, opts.Installed, opts.IncludeArchived) {
				continue
			}
			if active[repoPath] {
				if changedDeps, err := addDependencies(model, repoPath, active, opts, rootIDs); err != nil {
					return plan{}, err
				} else if changedDeps {
					changed = true
				}
				continue
			}
			if len(entry.Conditions) > 0 && conditionsMatch(entry.Conditions, active, opts.Installed, model) {
				active[repoPath] = true
				changed = true
			}
		}
	}

	activeFilesSet := activeFilesForRepoSet(model, active, opts.Installed, opts.InstalledFiles, opts.IncludeArchived)
	activeDirsSet := activeDirsForRepoSet(model, active, opts.Installed, opts.InstalledDirs, opts.IncludeArchived)
	return plan{
		Repos: sortedKeys(active),
		Files: orderFilesForApply(model, activeFilesSet),
		Dirs:  sortedKeys(activeDirsSet),
	}, nil
}

// activeDirsForRepoSet mirrors activeFilesForRepoSet for $dir entries, which
// have no links and therefore no ordering constraints.
func activeDirsForRepoSet(model *Model, activeRepos map[string]bool, installedRepos map[string]bool, installedDirs map[string]bool, includeArchived bool) map[string]bool {
	dirs := map[string]bool{}
	for _, dirPath := range sortedPathsOfKind(model, EntryDir) {
		entry, _ := model.entry(dirPath, EntryDir)
		if archivedExcluded(entry, installedDirs, includeArchived) {
			continue
		}
		if !entryActive(model, entry, activeRepos, installedRepos, installedDirs) {
			continue
		}
		dirs[dirPath] = true
	}
	return dirs
}

// entryActive decides whether a file or dir participates in plans: it is
// already installed (state records intent, like repos), its explicit
// conditions match, or - with no explicit conditions - a repository in its
// scope is active or installed.
func entryActive(model *Model, entry Entry, activeRepos map[string]bool, installedRepos map[string]bool, installedSelf map[string]bool) bool {
	if installedSelf[entry.Identity] {
		return true
	}
	if len(entry.Conditions) > 0 {
		return conditionsMatch(entry.Conditions, activeRepos, installedRepos, model)
	}
	return scopeActive(model, entry.Path, activeRepos, installedRepos)
}

// scopeActive reports whether any repository in the entry's scope is active
// or installed. The scope is the nearest ancestor path that contains
// repositories, so a support file placed next to a group of repos follows
// those repos; the workspace root is the last resort.
func scopeActive(model *Model, entryPath string, activeRepos map[string]bool, installedRepos map[string]bool) bool {
	scope := entryScope(model, entryPath)
	if scope == "" {
		if len(activeRepos) > 0 {
			return true
		}
		identityToPath := repoIdentityToPath(model)
		for identity := range installedRepos {
			if _, ok := identityToPath[identity]; ok {
				return true
			}
		}
		return false
	}
	return conditionMatches(Condition{Path: scope}, activeRepos, installedRepos, model)
}

// entryScope returns the nearest ancestor path of entryPath containing at
// least one repository, or "" for the workspace root.
func entryScope(model *Model, entryPath string) string {
	for scope := parentPath(entryPath); scope != ""; scope = parentPath(scope) {
		for _, repoPath := range sortedRepoPaths(model) {
			if pathMatches(scope, repoPath) {
				return scope
			}
		}
	}
	return ""
}

func parentPath(path string) string {
	idx := strings.LastIndex(path, "/")
	if idx < 0 {
		return ""
	}
	return path[:idx]
}

// archivedExcluded reports whether an archived entry should be left out of a
// plan: archived entries stay in only when requested or already installed.
func archivedExcluded(entry Entry, installed map[string]bool, includeArchived bool) bool {
	return entry.archived() && !includeArchived && !installed[entry.Identity]
}

func orderFilesForApply(model *Model, active map[string]bool) []string {
	visited := map[string]bool{}
	visiting := map[string]bool{}
	var result []string
	var visit func(string)
	visit = func(path string) {
		if visited[path] || !active[path] {
			return
		}
		if visiting[path] {
			return
		}
		visiting[path] = true
		if entry, ok := model.entry(path, EntryFile); ok && entry.File.Link != "" {
			visit(entry.File.Link)
		}
		visiting[path] = false
		visited[path] = true
		result = append(result, path)
	}
	for _, path := range sortedKeys(active) {
		visit(path)
	}
	return result
}

func addDependencies(model *Model, repoPath string, active map[string]bool, opts planOptions, excludedIDs map[string]bool) (bool, error) {
	entry, _ := model.entry(repoPath, EntryRepo)
	changed := false
	for _, dep := range entry.Repo.DependsOn {
		selection, err := model.Select(NodeQuery{Path: dep.Path, IncludeArchived: true})
		if err != nil {
			return false, fmt.Errorf("invalid dependency %s for %s: %w", dep.Path, repoPath, err)
		}
		matches := selection.ofKind(EntryRepo)
		if len(matches) == 0 {
			return false, fmt.Errorf("dependency %s for %s does not resolve to any repository", dep.Path, repoPath)
		}
		for _, matchEntry := range matches {
			match := matchEntry.Path
			if archivedExcluded(matchEntry, opts.Installed, opts.IncludeArchived) {
				continue
			}
			if dep.Optional && !opts.IncludeOptional && !(opts.IncludeInstalledOptional && opts.Installed[matchEntry.Identity]) {
				continue
			}
			if excludedIDs[matchEntry.Identity] {
				continue
			}
			if len(matchEntry.Conditions) > 0 && !conditionsMatch(matchEntry.Conditions, active, opts.Installed, model) {
				continue
			}
			if !active[match] {
				active[match] = true
				changed = true
			}
		}
	}
	return changed, nil
}

func activeFilesForRepoSet(model *Model, activeRepos map[string]bool, installedRepos map[string]bool, installedFiles map[string]bool, includeArchived bool) map[string]bool {
	files := map[string]bool{}
	changed := true
	for changed {
		changed = false
		for _, filePath := range sortedFilePaths(model) {
			if files[filePath] {
				continue
			}
			entry, _ := model.entry(filePath, EntryFile)
			if archivedExcluded(entry, installedFiles, includeArchived) {
				continue
			}
			if !entryActive(model, entry, activeRepos, installedRepos, installedFiles) {
				continue
			}
			if entry.File.Link != "" && !files[entry.File.Link] {
				continue
			}
			files[filePath] = true
			changed = true
		}
	}
	return files
}

func conditionsMatch(conditions []Condition, activeRepos map[string]bool, installed map[string]bool, model *Model) bool {
	for _, condition := range conditions {
		if !conditionMatches(condition, activeRepos, installed, model) {
			return false
		}
	}
	return true
}

func conditionMatches(condition Condition, activeRepos map[string]bool, installed map[string]bool, model *Model) bool {
	for repoPath := range activeRepos {
		if pathMatches(condition.Path, repoPath) {
			return true
		}
	}
	identityToPath := repoIdentityToPath(model)
	for identity := range installed {
		if repoPath, ok := identityToPath[identity]; ok && pathMatches(condition.Path, repoPath) {
			return true
		}
	}
	return false
}
