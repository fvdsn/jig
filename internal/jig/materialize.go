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
		if ok && (!entry.File.Archived || installed[entry.Identity]) {
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
	for _, repoPath := range plan.Repos {
		if err := ensureRepo(out, ws.Root, &ws.Model, &ws.State, repoPath, allowMove); err != nil {
			fmt.Fprintf(out, "skipped:\n  %s: %s\n", repoPath, err)
		}
	}
	for _, filePath := range plan.Files {
		if err := ensureFile(out, ws.Root, &ws.Model, &ws.State, filePath, allowMove); err != nil {
			fmt.Fprintf(out, "skipped:\n  %s: %s\n", filePath, err)
		}
	}
}
