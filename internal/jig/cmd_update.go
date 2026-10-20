package jig

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

type UpdateOptions struct {
	Sync            bool
	IncludeOptional bool
	SkipDeps        bool // sync only the selected repos, without their dependencies
	Prune           bool // delete entries removed from the schema during the sync step
}

// Update fast-forwards the schema source checkout to its upstream. The
// upstream schema is validated before the checkout is touched, so an invalid
// upstream never becomes the live definition. --sync is a legacy alias for
// jig sync, which updates the schema itself before applying it.
func Update(options UpdateOptions, out io.Writer) error {
	ws, err := loadWorkspace(options.Sync)
	if err != nil {
		return err
	}
	defer ws.Close()
	if err := updateSchema(ws, out); err != nil {
		return err
	}
	if options.Sync {
		return syncWorkspace(out, ws, SyncOptions{
			IncludeOptional: options.IncludeOptional,
			SkipDeps:        options.SkipDeps,
			Prune:           options.Prune,
		})
	}
	return nil
}

// updateSchema fast-forwards the schema checkout to its upstream, prints the
// definition changes, and swaps the workspace's live definition to the merged
// schema. On error the checkout and the loaded workspace are unchanged.
func updateSchema(ws *Workspace, out io.Writer) error {
	src := filepath.Join(ws.Root, sourceDir)
	if _, err := gitOrigin(src); err != nil {
		return fmt.Errorf("schema checkout has no origin remote (%s): %s", sourceDir, shortError(err))
	}
	if _, err := git(src, "fetch", "--quiet"); err != nil {
		return err
	}
	schemaPath := ws.Config.Schema
	data, err := git(src, "show", "@{upstream}:"+schemaPath)
	if err != nil {
		// The schema file may have been renamed upstream (jig.json to
		// jig.yaml, say): follow it to the first candidate name found.
		moved := ""
		for _, candidate := range schemaCandidates {
			if candidate == schemaPath {
				continue
			}
			if found, probe := git(src, "show", "@{upstream}:"+candidate); probe == nil {
				moved, data = candidate, found
				break
			}
		}
		if moved == "" {
			return fmt.Errorf("could not read upstream schema: %s", shortError(err))
		}
		schemaPath = moved
	}
	incoming, err := decodeDefinition([]byte(data), schemaPath)
	if err != nil {
		return fmt.Errorf("invalid upstream schema: %s", err)
	}
	validation := validateDefinition(incoming)
	if len(validation.Errors) > 0 {
		return validation.asError("invalid upstream schema")
	}
	if _, err := flattenDefinition(incoming, ws.Harnesses); err != nil {
		return err
	}

	// Whether the merge changed anything is read from HEAD rather than
	// from git's (localized) output.
	before, _ := git(src, "rev-parse", "HEAD")
	if _, err := git(src, "merge", "--ff-only", "@{upstream}"); err != nil {
		return fmt.Errorf("could not fast-forward schema checkout (%s): local history has diverged or local edits conflict; resolve with git in %s", shortError(err), sourceDir)
	}
	if after, _ := git(src, "rev-parse", "HEAD"); strings.TrimSpace(after) == strings.TrimSpace(before) {
		fmt.Fprintln(out, "schema already up to date")
	}
	if schemaPath != ws.Config.Schema {
		ws.Config.Schema = schemaPath
		if err := saveConfig(ws.Root, ws.Config); err != nil {
			return err
		}
		fmt.Fprintf(out, "schema file is now %s (recorded in .jig/config.json)\n", schemaPath)
	}

	// Diff against the live schema file after the merge; it may carry
	// uncommitted local edits on top of the upstream state.
	def, err := loadDefinition(ws.SchemaFile())
	if err != nil {
		return err
	}
	model, err := flattenDefinition(def, ws.Harnesses)
	if err != nil {
		return err
	}
	printDefinitionChanges(out, &ws.Model, &model)
	ws.Def = *def
	ws.Model = model
	ws.invalidateInstalled()
	return nil
}
