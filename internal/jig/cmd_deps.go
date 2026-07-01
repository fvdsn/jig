package jig

import (
	"fmt"
	"io"
)

type DependenciesOptions struct {
	Path            string
	IncludeOptional bool
	IncludeArchived bool
	Tags            []string
}

func Dependencies(options DependenciesOptions, out io.Writer) error {
	ws, err := loadWorkspace(false)
	if err != nil {
		return err
	}
	selection, err := ws.Select(NodeQuery{Path: options.Path, IncludeArchived: options.IncludeArchived, Tags: options.Tags})
	if err != nil {
		return err
	}
	roots := selection.repoPaths()
	if len(roots) == 0 {
		return fmt.Errorf("no repositories match %q", selection.Path)
	}
	installed := ws.installedNodes()
	plan, err := resolvePlan(&ws.Model, roots, planOptions{
		IncludeOptional: options.IncludeOptional,
		IncludeArchived: options.IncludeArchived,
		IncludeRoots:    false,
		Installed:       installed.Repos,
		InstalledFiles:  installed.Files,
	})
	if err != nil {
		return err
	}
	for _, dep := range plan.Repos {
		fmt.Fprintln(out, dep)
	}
	return nil
}
