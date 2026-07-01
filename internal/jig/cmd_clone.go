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
	selection, err := ws.Select(NodeQuery{Path: options.Path, IncludeArchived: options.IncludeArchived})
	if err != nil {
		return err
	}
	roots := selection.repoPaths()
	explicitFiles := selection.filePaths()
	if len(roots) == 0 && len(explicitFiles) == 0 {
		if selection.Path == "" {
			return errors.New("no repositories or files defined")
		}
		return fmt.Errorf("no repositories or files match %q", selection.Path)
	}
	return resolveAndApplyPlan(out, ws, roots, explicitFiles, applyOptions{
		IncludeOptional: options.IncludeOptional,
		IncludeArchived: options.IncludeArchived,
		RefreshFiles:    options.Refresh,
	})
}
