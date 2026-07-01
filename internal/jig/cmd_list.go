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
	for _, entry := range selection.Entries {
		fmt.Fprintf(out, "%-5s %s", entry.Kind, entry.Path)
		if description := entry.description(); description != "" {
			fmt.Fprintf(out, "\t%s", description)
		}
		fmt.Fprintln(out)
	}
	return nil
}
