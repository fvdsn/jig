package jig

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"
)

type UpdateOptions struct {
	Sync            bool
	Path            string
	IncludeOptional bool
	IncludeArchived bool
}

func Update(options UpdateOptions, out io.Writer) error {
	ws, err := loadWorkspace(options.Sync)
	if err != nil {
		return err
	}
	if ws.Def.Source == nil {
		return errors.New(".jig.json has no source")
	}
	source := *ws.Def.Source
	if source.Type != "git" {
		return fmt.Errorf("unsupported source type %q", source.Type)
	}
	if source.Path == "" {
		source.Path = definitionFile
	}
	if err := validateSafePath(source.Path); err != nil {
		return fmt.Errorf("invalid source path: %s", err)
	}
	if source.Ref == "" {
		ref, err := discoverDefaultBranch(source.URL)
		if err != nil {
			return fmt.Errorf("could not determine default branch for %s", source.URL)
		}
		source.Ref = ref
	}

	incoming, err := fetchDefinition(source.URL, source.Ref, source.Path)
	if err != nil {
		return err
	}
	incoming.Source = &source
	validation := validateDefinition(incoming)
	if len(validation.Errors) > 0 {
		return validation.asError("invalid incoming definition")
	}
	incomingModel, err := flattenDefinition(incoming)
	if err != nil {
		return err
	}
	printDefinitionChanges(out, &ws.Model, &incomingModel)
	if err := writeJSON(filepath.Join(ws.Root, definitionFile), incoming); err != nil {
		return err
	}
	if options.Sync {
		ws.Def = *incoming
		ws.Model = incomingModel
		return syncWorkspace(out, ws, options.Path, options.IncludeOptional, options.IncludeArchived)
	}
	return nil
}
