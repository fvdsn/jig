package jig

import (
	"fmt"
	"io"
)

// resolveAndApplyPlan expands roots into a full plan and materializes it.
// When syncing, installed optional dependencies stay included and existing
// checkouts may be moved to their new paths.
func resolveAndApplyPlan(out io.Writer, ws *Workspace, roots []string, explicitFiles []string, includeOptional bool, includeArchived bool, syncing bool) error {
	installed := ws.installedNodes()
	plan, err := resolvePlan(&ws.Model, roots, planOptions{
		IncludeOptional:          includeOptional,
		IncludeInstalledOptional: syncing,
		IncludeArchived:          includeArchived,
		IncludeRoots:             true,
		Installed:                installed.Repos,
		InstalledFiles:           installed.Files,
	})
	if err != nil {
		return err
	}
	plan = includeExplicitFiles(&ws.Model, plan, explicitFiles)
	if !includeArchived {
		plan = excludeArchivedFiles(&ws.Model, plan, installed.Files)
	}
	applyPlan(out, ws, plan, syncing)
	return nil
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

func applyPlan(out io.Writer, ws *Workspace, plan plan, allowMove bool) {
	// Repositories are independent of each other, so the git work runs in
	// parallel; state and output updates are applied serially in plan order.
	entries := make([]Entry, len(plan.Repos))
	results := make([]ensureRepoResult, len(plan.Repos))
	for i, repoPath := range plan.Repos {
		entries[i], _ = ws.Model.entry(repoPath, EntryRepo)
	}
	forEachParallel(len(plan.Repos), func(i int) {
		stateRepo, hasState := ws.State.Repos[entries[i].Identity]
		results[i] = ensureRepo(ws.Root, entries[i], stateRepo, hasState, allowMove)
	})
	for i, result := range results {
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
	}
	for _, filePath := range plan.Files {
		if err := ensureFile(out, ws.Root, &ws.Model, &ws.State, filePath, allowMove); err != nil {
			fmt.Fprintf(out, "skipped:\n  %s: %s\n", filePath, err)
		}
	}
}
