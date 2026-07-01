package jig

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type InitOptions struct {
	SourceArg       string
	WorkspaceDir    string
	DefinitionPath  string
	Clone           bool
	ClonePath       string
	IncludeOptional bool
	IncludeArchived bool
}

func Init(options InitOptions, out io.Writer) error {
	workspaceDir := options.WorkspaceDir
	workspaceDir, err := filepath.Abs(workspaceDir)
	if err != nil {
		return err
	}

	definitionPath := options.DefinitionPath
	if err := validateSafePath(definitionPath); err != nil {
		return fmt.Errorf("invalid definition path: %s", err)
	}

	if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
		return err
	}
	localDefinitionPath := filepath.Join(workspaceDir, definitionFile)
	if _, err := os.Stat(localDefinitionPath); err == nil {
		return errors.New("workspace already initialized: .jig.json exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	def, err := loadInitDefinition(options.SourceArg, definitionPath)
	if err != nil {
		return err
	}

	validation := validateDefinition(def)
	if len(validation.Errors) > 0 {
		return validation.asError("invalid fetched definition")
	}
	model, err := flattenDefinition(def)
	if err != nil {
		return err
	}

	if err := writeJSON(localDefinitionPath, def); err != nil {
		return err
	}
	if err := saveState(workspaceDir, emptyState()); err != nil {
		return err
	}

	fmt.Fprintf(out, "initialized workspace at %s\n", workspaceDir)
	if options.Clone {
		state, err := loadState(workspaceDir)
		if err != nil {
			return err
		}
		ws := Workspace{Root: workspaceDir, Def: *def, Model: model, State: state}
		if err := clonePathIntoWorkspace(out, &ws, options.ClonePath, options.IncludeOptional, options.IncludeArchived); err != nil {
			return err
		}
		if err := saveState(workspaceDir, ws.State); err != nil {
			return err
		}
	}
	return nil
}

func loadInitDefinition(sourceArg string, definitionPath string) (*Definition, error) {
	if info, err := os.Stat(sourceArg); err == nil && !info.IsDir() {
		if definitionPath != definitionFile {
			return nil, errors.New("--path can only be used with Git sources")
		}
		def, err := loadDefinition(sourceArg)
		if err != nil {
			return nil, err
		}
		def.Source = nil
		return def, nil
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	ref, err := discoverDefaultBranch(sourceArg)
	if err != nil {
		return nil, fmt.Errorf("could not determine default branch for %s", sourceArg)
	}
	def, err := fetchDefinition(sourceArg, ref, definitionPath)
	if err != nil {
		return nil, err
	}
	def.Source = &Source{Type: "git", URL: sourceArg, Ref: ref, Path: definitionPath}
	return def, nil
}
