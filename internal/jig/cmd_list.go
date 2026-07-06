package jig

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"
)

type ListOptions struct {
	Path            string
	IncludeArchived bool
	Tags            []string
	Width           int // output width; 0 auto-detects, <0 or non-terminal output disables truncation
}

func List(options ListOptions, out io.Writer) error {
	ws, err := loadWorkspace(false)
	if err != nil {
		return err
	}
	query := NodeQuery{Path: options.Path, IncludeArchived: options.IncludeArchived, Tags: options.Tags}
	selection, err := ws.Select(query)
	if err != nil {
		return err
	}
	width := options.Width
	if width == 0 {
		width = outputWidth(out)
	}
	if width <= 0 {
		// Machine-friendly output for pipes: full descriptions, tab-separated.
		for _, entry := range selection.Entries {
			fmt.Fprintf(out, "%-5s %s", entry.Kind, entry.Path)
			if description := entry.description(); description != "" {
				fmt.Fprintf(out, "\t%s", description)
			}
			fmt.Fprintln(out)
		}
		return nil
	}

	// Terminal output: aligned columns, one line per entry, descriptions
	// collapsed and truncated to the terminal width.
	maxPath := 0
	for _, entry := range selection.Entries {
		if w := utf8.RuneCountInString(entry.Path); w > maxPath {
			maxPath = w
		}
	}
	// Cap the path column so a few long outlier paths do not starve the
	// description column; rows with longer paths go ragged instead.
	if limit := width * 45 / 100; maxPath > limit && limit >= 20 {
		maxPath = limit
	}
	for _, entry := range selection.Entries {
		line := fmt.Sprintf("%-5s %-*s", entry.Kind, maxPath, entry.Path)
		if description := strings.Join(strings.Fields(entry.description()), " "); description != "" {
			available := width - utf8.RuneCountInString(line) - 2
			if available >= 4 {
				line += "  " + truncateRunes(description, available)
			}
		}
		fmt.Fprintln(out, strings.TrimRight(line, " "))
	}
	return nil
}

// outputWidth returns the terminal width of out, preferring an explicit
// COLUMNS variable; 0 when out is not a terminal.
func outputWidth(out io.Writer) int {
	if columns := os.Getenv("COLUMNS"); columns != "" {
		if width, err := strconv.Atoi(columns); err == nil && width > 0 {
			return width
		}
	}
	if file, ok := out.(*os.File); ok {
		return termWidth(file)
	}
	return 0
}

func truncateRunes(text string, limit int) string {
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	runes := []rune(text)
	return string(runes[:limit-1]) + "…"
}
