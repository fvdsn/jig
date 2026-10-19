package jig

import (
	"fmt"
	"io"
)

type SyncOptions struct {
	IncludeOptional bool
	SkipDeps        bool // sync only the installed repos, without their dependencies
	SkipUpdate      bool // apply the current schema without updating it first
	Prune           bool // delete entries removed from the schema (jig rm safety rules apply)
}

// Sync updates the schema checkout, then applies it. A schema update that
// fails (unreachable remote, invalid or diverged upstream) is reported and
// the current schema is applied instead, so sync still converges offline;
// --no-update skips the update step deliberately.
func Sync(options SyncOptions, out io.Writer) error {
	ws, err := loadWorkspace(true)
	if err != nil {
		return err
	}
	defer ws.Close()
	if !options.SkipUpdate {
		if err := updateSchema(ws, out); err != nil {
			fmt.Fprintf(out, "schema not updated: %s\n", shortError(err))
		}
	}
	return syncWorkspace(out, ws, options)
}

func syncWorkspace(out io.Writer, ws *Workspace, options SyncOptions) error {
	// A schema id rename leaves the old identity in state while the new one
	// claims the same path; move the records first so the plan below sees
	// the checkout as already tracked instead of stale.
	readoptRenamedIdentities(out, &ws.Model, &ws.State)

	if options.Prune && ws.Subdir != "" {
		return fmt.Errorf("--prune applies to the whole workspace; run it from the workspace root")
	}
	// Sync converges what is already installed, scoped to where the command
	// runs; jig clone is how entries are added.
	scope, err := ws.ResolvePath("")
	if err != nil {
		return err
	}
	var roots []string
	for _, repoPath := range desiredDefinedRepos(ws.Root, &ws.Model, &ws.State) {
		if nodePathMatches(scope, repoPath) {
			roots = append(roots, repoPath)
		}
	}

	// A skip failure still leaves valid work behind: report stale entries,
	// keep the state, and surface the failure in the exit code.
	applyErr := resolveAndApplyPlan(out, ws, roots, nil, nil, applyOptions{
		IncludeOptional: options.IncludeOptional,
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
