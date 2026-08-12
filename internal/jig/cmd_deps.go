package jig

import (
	"fmt"
	"io"
)

type DependenciesOptions struct {
	Path            string
	Id              string // selects one entry by identity instead of a path
	Reverse         bool   // list direct dependents instead of recursive dependencies
	IncludeOptional bool
	IncludeArchived bool
	Tags            []string
}

func Dependencies(options DependenciesOptions, out io.Writer) error {
	ws, err := loadWorkspace(false)
	if err != nil {
		return err
	}
	selection, err := ws.Select(NodeQuery{Path: options.Path, Id: options.Id, IncludeArchived: options.IncludeArchived, Tags: options.Tags})
	if err != nil {
		return err
	}
	roots := selection.repoPaths()
	if len(roots) == 0 {
		return fmt.Errorf("no repositories match %q", selection.Path)
	}
	installed := ws.installedNodes()
	if options.Reverse {
		printReverseDependents(out, ws, roots, installed, options)
		return nil
	}
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

// printReverseDependents lists repositories with a direct dependency edge
// onto any of the target repositories. Unlike forward resolution this is
// deliberately not recursive: the answer is the declared consumers, not the
// transitive blast radius. Edges through group paths count, so a repo
// depending on "platform" is a direct dependent of every repo under it.
func printReverseDependents(out io.Writer, ws *Workspace, targets []string, installed InstalledNodes, options DependenciesOptions) {
	targetSet := map[string]bool{}
	for _, target := range targets {
		targetSet[target] = true
	}
	for _, repoPath := range sortedRepoPaths(&ws.Model) {
		entry := ws.Model.Entries[repoPath]
		if entry.archived() && !options.IncludeArchived && !installed.Repos[entry.Identity] {
			continue
		}
		if dependsOnAny(&ws.Model, entry, targetSet, options.IncludeOptional) {
			fmt.Fprintln(out, repoPath)
		}
	}
}

// dependsOnAny reports whether the repository declares (or inherits) a
// dependency resolving to any target other than itself.
func dependsOnAny(model *Model, entry Entry, targets map[string]bool, includeOptional bool) bool {
	for _, dep := range entry.Repo.DependsOn {
		if dep.Optional && !includeOptional {
			continue
		}
		for _, match := range model.resolveRepoRef(dep.Ref) {
			if match.Path != entry.Path && targets[match.Path] {
				return true
			}
		}
	}
	return false
}
