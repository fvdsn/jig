package jig

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

type UpdateOptions struct {
	Sync            bool
	Path            string
	IncludeOptional bool
	IncludeArchived bool
	SkipDeps        bool // sync only the selected repos, without their dependencies
	Prune           bool // delete entries removed from the schema during the sync step
	Tags            []string
}

// Update fast-forwards the schema source checkout to its upstream. The
// upstream schema is validated before the checkout is touched, so an invalid
// upstream never becomes the live definition.
func Update(options UpdateOptions, out io.Writer) error {
	ws, err := loadWorkspace(options.Sync)
	if err != nil {
		return err
	}
	src := filepath.Join(ws.Root, sourceDir)
	if _, err := gitOrigin(src); err != nil {
		return fmt.Errorf("schema checkout has no origin remote (%s): %s", sourceDir, shortError(err))
	}
	if _, err := git(src, "fetch", "--quiet"); err != nil {
		return err
	}
	data, err := git(src, "show", "@{upstream}:"+ws.Config.Schema)
	if err != nil {
		return fmt.Errorf("could not read upstream schema: %s", shortError(err))
	}
	var incoming Definition
	if err := json.Unmarshal([]byte(data), &incoming); err != nil {
		return fmt.Errorf("invalid upstream schema: %s", err)
	}
	validation := validateDefinition(&incoming)
	if len(validation.Errors) > 0 {
		return validation.asError("invalid upstream schema")
	}
	if _, err := flattenDefinition(&incoming); err != nil {
		return err
	}

	if mergeOut, err := git(src, "merge", "--ff-only", "@{upstream}"); err != nil {
		return fmt.Errorf("could not fast-forward schema checkout: local history has diverged or local edits conflict; resolve with git in %s", sourceDir)
	} else if strings.Contains(mergeOut, "Already up to date") {
		fmt.Fprintln(out, "schema already up to date")
	}

	// Diff and sync against the live schema file after the merge; it may
	// carry uncommitted local edits on top of the upstream state.
	def, err := loadDefinition(ws.SchemaFile())
	if err != nil {
		return err
	}
	model, err := flattenDefinition(def)
	if err != nil {
		return err
	}
	printDefinitionChanges(out, &ws.Model, &model)
	if options.Sync {
		ws.Def = *def
		ws.Model = model
		return syncWorkspace(out, ws, SyncOptions{
			Path:            options.Path,
			IncludeOptional: options.IncludeOptional,
			IncludeArchived: options.IncludeArchived,
			SkipDeps:        options.SkipDeps,
			Prune:           options.Prune,
			Tags:            options.Tags,
		})
	}
	return nil
}
