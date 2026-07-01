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
}

func Clone(options CloneOptions, out io.Writer) error {
	ws, err := loadWorkspace(true)
	if err != nil {
		return err
	}
	if err := clonePathIntoWorkspace(out, ws, options.Path, options.IncludeOptional, options.IncludeArchived); err != nil {
		return err
	}
	return saveState(ws.Root, ws.State)
}

func clonePathIntoWorkspace(out io.Writer, ws *Workspace, path string, includeOptional bool, includeArchived bool) error {
	selection, err := ws.Select(NodeQuery{Path: path, IncludeArchived: includeArchived})
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
	installed := ws.installedNodes()
	plan, err := resolvePlan(&ws.Model, roots, planOptions{
		IncludeOptional: includeOptional,
		IncludeArchived: includeArchived,
		IncludeRoots:    true,
		Installed:       installed.Repos,
		InstalledFiles:  installed.Files,
	})
	if err != nil {
		return err
	}
	plan = includeExplicitFiles(&ws.Model, plan, explicitFiles)
	if !includeArchived {
		plan = excludeArchivedFiles(&ws.Model, plan, installed.Files)
	}
	applyPlan(out, ws, plan, false)
	return nil
}
