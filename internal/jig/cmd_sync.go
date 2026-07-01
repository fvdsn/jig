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
	if options.Path != "" {
		selection, err := ws.Select(NodeQuery{Path: options.Path, IncludeArchived: options.IncludeArchived})
		if err != nil {
			return err
		}
		roots = selection.repoPaths()
		explicitFiles = selection.filePaths()
		if len(roots) == 0 && len(explicitFiles) == 0 {
			return fmt.Errorf("no repositories or files match %q", selection.Path)
		}
	} else {
		roots = installedDefinedRepos(ws.Root, &ws.Model, &ws.State)
	}

	if err := resolveAndApplyPlan(out, ws, roots, explicitFiles, applyOptions{
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
