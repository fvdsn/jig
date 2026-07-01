package jig

import "fmt"

type planOptions struct {
	IncludeOptional          bool
	IncludeInstalledOptional bool
	IncludeArchived          bool
	IncludeRoots             bool
	Installed                map[string]bool
	InstalledFiles           map[string]bool
}

type plan struct {
	Repos []string
	Files []string
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
		if opts.IncludeRoots && !repoArchived(entry, opts) {
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
			if repoArchived(entry, opts) {
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
	return plan{Repos: sortedKeys(active), Files: orderFilesForApply(model, activeFilesSet)}, nil
}

func repoArchived(entry Entry, opts planOptions) bool {
	return entry.Repo.Archived && !opts.IncludeArchived && !opts.Installed[entry.Identity]
}

func fileArchived(entry Entry, installed map[string]bool, includeArchived bool) bool {
	return entry.File.Archived && !includeArchived && !installed[entry.Identity]
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
			if repoArchived(matchEntry, opts) {
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
			if fileArchived(entry, installedFiles, includeArchived) {
				continue
			}
			if len(entry.Conditions) > 0 && !conditionsMatch(entry.Conditions, activeRepos, installedRepos, model) {
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
		condition := condition
		if !conditionMatches(&condition, activeRepos, installed, model) {
			return false
		}
	}
	return true
}

func conditionMatches(condition *Condition, activeRepos map[string]bool, installed map[string]bool, model *Model) bool {
	if condition == nil {
		return true
	}
	for repoPath := range activeRepos {
		if pathMatches(condition.Path, repoPath) {
			return true
		}
	}
	for identity := range installed {
		if repoPath, ok := repoIdentityToPath(model)[identity]; ok && pathMatches(condition.Path, repoPath) {
			return true
		}
	}
	return false
}
