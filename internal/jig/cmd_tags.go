package jig

import (
	"fmt"
	"io"
	"sort"
	"unicode/utf8"
)

type TagsOptions struct {
	Path            string
	IncludeArchived bool
}

// Tags lists the tag vocabulary of the selected entries, with the number of
// entries carrying each tag. Counts match what filtering on the same tag
// would select, group entries included.
func Tags(options TagsOptions, out io.Writer) error {
	ws, err := loadWorkspace(false)
	if err != nil {
		return err
	}
	selection, err := ws.Select(NodeQuery{Path: options.Path, IncludeArchived: options.IncludeArchived})
	if err != nil {
		return err
	}
	counts := map[string]int{}
	for _, entry := range selection.Entries {
		for _, tag := range entry.Tags {
			counts[tag]++
		}
	}
	names := make([]string, 0, len(counts))
	width := 0
	for name := range counts {
		names = append(names, name)
		if w := utf8.RuneCountInString(name); w > width {
			width = w
		}
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Fprintf(out, "%-*s  %d\n", width, name, counts[name])
	}
	return nil
}
