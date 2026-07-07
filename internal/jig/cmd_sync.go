package jig

import (
	"fmt"
	"io"
)

type SyncOptions struct {
	Path            string
	IncludeOptional bool
	IncludeArchived bool
	SkipDeps        bool // sync only the selected repos, without their dependencies
	Prune           bool // delete entries removed from the schema (jig rm safety rules apply)
	Tags            []string
}

func Sync(options SyncOptions, out io.Writer) error {
	ws, err := loadWorkspace(true)
	if err != nil {
		return err
	}
	defer ws.Close()
	return syncWorkspace(out, ws, options)
}

func syncWorkspace(out io.Writer, ws *Workspace, options SyncOptions) error {
	// A schema id rename leaves the old identity in state while the new one
	// claims the same path; move the records first so the plan below sees
	// the checkout as already tracked instead of stale.
	readoptRenamedIdentities(out, &ws.Model, &ws.State)

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

	// A skip failure still leaves valid work behind: report stale entries,
	// keep the state, and surface the failure in the exit code.
	applyErr := resolveAndApplyPlan(out, ws, roots, explicitFiles, explicitDirs, applyOptions{
		IncludeOptional: options.IncludeOptional,
		IncludeArchived: options.IncludeArchived,
		SkipDeps:        options.SkipDeps,
		Sync:            true,
	})
	if options.Prune {
		pruneStale(out, ws.Root, &ws.Model, &ws.State)
	} else {
		reportStale(out, ws.Root, &ws.Model, &ws.State)
	}
	if err := saveState(ws.Root, ws.State); err != nil {
		return err
	}
	return applyErr
}
