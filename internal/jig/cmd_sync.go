package jig

import (
	"fmt"
	"io"
)

type SyncOptions struct {
	Path            string
	IncludeOptional bool
	IncludeArchived bool
}

func Sync(options SyncOptions, out io.Writer) error {
	ws, err := loadWorkspace(true)
	if err != nil {
		return err
	}
	return syncWorkspace(out, ws, options.Path, options.IncludeOptional, options.IncludeArchived)
}

func syncWorkspace(out io.Writer, ws *Workspace, path string, includeOptional bool, includeArchived bool) error {
	var roots []string
	var explicitFiles []string
	if path != "" {
		selection, err := ws.Select(NodeQuery{Path: path, IncludeArchived: includeArchived})
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

	if err := resolveAndApplyPlan(out, ws, roots, explicitFiles, includeOptional, includeArchived, true); err != nil {
		return err
	}
	reportStale(out, ws.Root, &ws.Model, &ws.State)
	return saveState(ws.Root, ws.State)
}
