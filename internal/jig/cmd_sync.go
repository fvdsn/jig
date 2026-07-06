package jig

import (
	"fmt"
	"io"
)

type SyncOptions struct {
	Path            string
	IncludeOptional bool
	IncludeArchived bool
	Refresh         bool
	Tags            []string
}

func Sync(options SyncOptions, out io.Writer) error {
	ws, err := loadWorkspace(true)
	if err != nil {
		return err
	}
	return syncWorkspace(out, ws, options)
}

func syncWorkspace(out io.Writer, ws *Workspace, options SyncOptions) error {
	var roots []string
	var explicitFiles []string
	var explicitDirs []string
	if options.Path != "" {
		selection, err := ws.Select(NodeQuery{Path: options.Path, IncludeArchived: options.IncludeArchived, Tags: options.Tags})
		if err != nil {
			return err
		}
		roots = selection.repoPaths()
		explicitFiles = selection.filePaths()
		explicitDirs = entryPaths(selection.ofKind(EntryDir))
		if len(roots) == 0 && len(explicitFiles) == 0 && len(explicitDirs) == 0 {
			return fmt.Errorf("no repositories or files match %s", describeQuery(selection.Path, options.Tags))
		}
	} else {
		roots = desiredDefinedRepos(ws.Root, &ws.Model, &ws.State)
		if len(options.Tags) > 0 {
			var tagged []string
			for _, repoPath := range roots {
				if entry, ok := ws.Model.entry(repoPath, EntryRepo); ok && entry.hasAllTags(options.Tags) {
					tagged = append(tagged, repoPath)
				}
			}
			roots = tagged
		}
	}

	if err := resolveAndApplyPlan(out, ws, roots, explicitFiles, explicitDirs, applyOptions{
		IncludeOptional: options.IncludeOptional,
		IncludeArchived: options.IncludeArchived,
		Sync:            true,
		RefreshFiles:    options.Refresh,
	}); err != nil {
		return err
	}
	reportStale(out, ws.Root, &ws.Model, &ws.State)
	return saveState(ws.Root, ws.State)
}
