package jig

import (
	"fmt"
	"io"
	"sync"
)

type applyOptions struct {
	IncludeOptional bool
	IncludeArchived bool
	SkipDeps        bool // materialize only the roots, without their dependencies
	Sync            bool // keep installed optional deps and allow moving installed entries
	RefreshFiles    bool // refetch file content even when the local copy is unmodified
}

// resolveAndApplyPlan expands roots into a full plan and materializes it.
func resolveAndApplyPlan(out io.Writer, ws *Workspace, roots []string, explicitFiles []string, explicitDirs []string, opts applyOptions) error {
	installed := ws.installedNodes()
	plan, err := resolvePlan(&ws.Model, roots, planOptions{
		IncludeOptional:          opts.IncludeOptional,
		IncludeInstalledOptional: opts.Sync,
		IncludeArchived:          opts.IncludeArchived,
		IncludeRoots:             true,
		SkipDeps:                 opts.SkipDeps,
		Installed:                installed.Repos,
		InstalledFiles:           installed.Files,
		InstalledDirs:            installed.Dirs,
	})
	if err != nil {
		return err
	}
	plan = includeExplicitFiles(&ws.Model, plan, explicitFiles)
	plan = includeExplicitDirs(&ws.Model, plan, explicitDirs)
	if !opts.IncludeArchived {
		plan = excludeArchivedFiles(&ws.Model, plan, installed.Files)
		plan = excludeArchivedDirs(&ws.Model, plan, installed.Dirs)
	}
	applyPlan(out, ws, plan, opts, installed.Repos)
	return nil
}

func includeExplicitDirs(model *Model, base plan, dirs []string) plan {
	active := map[string]bool{}
	for _, dirPath := range base.Dirs {
		active[dirPath] = true
	}
	var add func(string)
	add = func(dirPath string) {
		entry, ok := model.entry(dirPath, EntryDir)
		if !ok || active[dirPath] {
			return
		}
		active[dirPath] = true
		if entry.Dir.Link != "" {
			add(entry.Dir.Link)
		}
	}
	for _, dirPath := range dirs {
		add(dirPath)
	}
	base.Dirs = orderDirsForApply(model, active)
	return base
}

func excludeArchivedDirs(model *Model, base plan, installed map[string]bool) plan {
	active := map[string]bool{}
	for _, dirPath := range base.Dirs {
		entry, ok := model.entry(dirPath, EntryDir)
		if ok && !archivedExcluded(entry, installed, false) {
			active[dirPath] = true
		}
	}
	// Drop links whose target dropped out.
	changed := true
	for changed {
		changed = false
		for dirPath := range active {
			entry, _ := model.entry(dirPath, EntryDir)
			if entry.Dir.Link != "" && !active[entry.Dir.Link] {
				delete(active, dirPath)
				changed = true
			}
		}
	}
	base.Dirs = orderDirsForApply(model, active)
	return base
}

func includeExplicitFiles(model *Model, base plan, files []string) plan {
	active := map[string]bool{}
	for _, filePath := range base.Files {
		active[filePath] = true
	}
	var add func(string)
	add = func(filePath string) {
		entry, ok := model.entry(filePath, EntryFile)
		if !ok {
			return
		}
		if entry.File.Link != "" {
			add(entry.File.Link)
		}
		active[filePath] = true
	}
	for _, filePath := range files {
		add(filePath)
	}
	base.Files = orderFilesForApply(model, active)
	return base
}

func excludeArchivedFiles(model *Model, base plan, installed map[string]bool) plan {
	active := map[string]bool{}
	for _, filePath := range base.Files {
		entry, ok := model.entry(filePath, EntryFile)
		if ok && !archivedExcluded(entry, installed, false) {
			active[filePath] = true
		}
	}
	changed := true
	for changed {
		changed = false
		for filePath := range active {
			entry, _ := model.entry(filePath, EntryFile)
			if entry.File.Link != "" && !active[entry.File.Link] {
				delete(active, filePath)
				changed = true
			}
		}
	}
	base.Files = orderFilesForApply(model, active)
	return base
}

func applyPlan(out io.Writer, ws *Workspace, plan plan, opts applyOptions, installedRepos map[string]bool) {
	// Repositories are independent of each other, so the git work runs in
	// parallel; each result is applied to state and printed as it completes,
	// so long runs show progress instead of a report at the end.
	entries := make([]Entry, len(plan.Repos))
	for i, repoPath := range plan.Repos {
		entries[i], _ = ws.Model.entry(repoPath, EntryRepo)
	}
	var mu sync.Mutex
	forEachParallel(len(plan.Repos), func(i int) {
		mu.Lock()
		stateRepo, hasState := ws.State.Repos[entries[i].Identity]
		mu.Unlock()
		result := ensureRepo(ws.Root, entries[i], stateRepo, hasState, opts.Sync)
		mu.Lock()
		defer mu.Unlock()
		if result.Remove {
			delete(ws.State.Repos, entries[i].Identity)
		}
		if result.StateRepo != nil {
			ws.State.Repos[entries[i].Identity] = *result.StateRepo
		}
		for _, message := range result.Messages {
			fmt.Fprintln(out, message)
		}
		if result.Err != nil {
			fmt.Fprintf(out, "skipped:\n  %s: %s\n", plan.Repos[i], result.Err)
		}
	})
	fetcher := newFileFetcher()
	for _, filePath := range plan.Files {
		if err := ensureFile(out, ws.Root, &ws.Model, &ws.State, filePath, opts.Sync, opts.RefreshFiles, fetcher); err != nil {
			fmt.Fprintf(out, "skipped:\n  %s: %s\n", filePath, err)
		}
	}
	activeRepos := map[string]bool{}
	for _, repoPath := range plan.Repos {
		activeRepos[repoPath] = true
	}
	for _, dirPath := range plan.Dirs {
		if err := ensureDir(out, ws.Root, &ws.Model, &ws.State, dirPath, opts.Sync, opts.RefreshFiles, fetcher, activeRepos, installedRepos); err != nil {
			fmt.Fprintf(out, "skipped:\n  %s: %s\n", dirPath, err)
		}
	}
}
