package jig

import (
	"errors"
	"fmt"
	"io"
)

type CloneOptions struct {
	Path            string
	IncludeOptional bool
	IncludeArchived bool
	Refresh         bool
	Tags            []string
}

func Clone(options CloneOptions, out io.Writer) error {
	ws, err := loadWorkspace(true)
	if err != nil {
		return err
	}
	if err := clonePathIntoWorkspace(out, ws, options); err != nil {
		return err
	}
	return saveState(ws.Root, ws.State)
}

func clonePathIntoWorkspace(out io.Writer, ws *Workspace, options CloneOptions) error {
	selection, err := ws.Select(NodeQuery{Path: options.Path, IncludeArchived: options.IncludeArchived, Tags: options.Tags})
	if err != nil {
		return err
	}
	roots := selection.repoPaths()
	explicitFiles := selection.filePaths()
	explicitDirs := entryPaths(selection.ofKind(EntryDir))
	if len(roots) == 0 && len(explicitFiles) == 0 && len(explicitDirs) == 0 {
		if selection.Path == "" && len(options.Tags) == 0 {
			return errors.New("no repositories or files defined")
		}
		return fmt.Errorf("no repositories or files match %s", describeQuery(selection.Path, options.Tags))
	}
	return resolveAndApplyPlan(out, ws, roots, explicitFiles, explicitDirs, applyOptions{
		IncludeOptional: options.IncludeOptional,
		IncludeArchived: options.IncludeArchived,
		RefreshFiles:    options.Refresh,
	})
}
