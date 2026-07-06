package jig

import (
	"fmt"
	"io"
	"sync"
)

type applyOptions struct {
	IncludeOptional bool
	IncludeArchived bool
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
		Installed:                installed.Repos,
		InstalledFiles:           installed.Files,
		InstalledDirs:            installed.Dirs,
	})
	if err != nil {
		return err
	}
	plan = includeExplicitFiles(&ws.Model, plan, explicitFiles)
	plan = includeExplicitDirs(plan, explicitDirs)
	if !opts.IncludeArchived {
		plan = excludeArchivedFiles(&ws.Model, plan, installed.Files)
		plan = excludeArchivedDirs(&ws.Model, plan, installed.Dirs)
	}
	applyPlan(out, ws, plan, opts)
	return nil
}

func includeExplicitDirs(base plan, dirs []string) plan {
	active := map[string]bool{}
	for _, dirPath := range base.Dirs {
		active[dirPath] = true
	}
	for _, dirPath := range dirs {
		active[dirPath] = true
	}
	base.Dirs = sortedKeys(active)
	return base
}

func excludeArchivedDirs(model *Model, base plan, installed map[string]bool) plan {
	var kept []string
	for _, dirPath := range base.Dirs {
		entry, ok := model.entry(dirPath, EntryDir)
		if ok && !archivedExcluded(entry, installed, false) {
			kept = append(kept, dirPath)
		}
	}
	base.Dirs = kept
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

func applyPlan(out io.Writer, ws *Workspace, plan plan, opts applyOptions) {
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
	for _, dirPath := range plan.Dirs {
		if err := ensureDir(out, ws.Root, &ws.Model, &ws.State, dirPath, opts.Sync, opts.RefreshFiles, fetcher); err != nil {
			fmt.Fprintf(out, "skipped:\n  %s: %s\n", dirPath, err)
		}
	}
}
