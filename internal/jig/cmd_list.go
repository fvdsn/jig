package jig

import (
	"fmt"
	"io"
)

type ListOptions struct {
	Path            string
	IncludeArchived bool
}

func List(options ListOptions, out io.Writer) error {
	ws, err := loadWorkspace(false)
	if err != nil {
		return err
	}
	query := NodeQuery{Path: options.Path, IncludeArchived: options.IncludeArchived}
	selection, err := ws.Select(query)
	if err != nil {
		return err
	}
	for _, entry := range selection.Repos {
		repo := entry.Repo
		fmt.Fprintf(out, "repo  %s", entry.Path)
		if repo.Description != "" {
			fmt.Fprintf(out, "\t%s", repo.Description)
		}
		fmt.Fprintln(out)
	}
	for _, entry := range selection.Files {
		file := entry.File
		fmt.Fprintf(out, "file  %s", entry.Path)
		if file.Description != "" {
			fmt.Fprintf(out, "\t%s", file.Description)
		}
		fmt.Fprintln(out)
	}
	return nil
}
