package jig

import (
	"fmt"
	"io"
	"strings"
)

type PullOptions struct {
	Path            string
	IncludeArchived bool
}

func Pull(options PullOptions, out io.Writer) error {
	ws, err := loadWorkspace(false)
	if err != nil {
		return err
	}
	query := NodeQuery{Path: options.Path, IncludeArchived: options.IncludeArchived}
	selection, err := ws.Select(query)
	if err != nil {
		return err
	}

	var pulled []string
	var skipped []string
	for _, entry := range selection.ofKind(EntryRepo) {
		repoPath := entry.Path
		local, ok := installedPath(ws.Root, &ws.Model, &ws.State, repoPath)
		if !ok {
			continue
		}
		fmt.Fprintf(out, "pulling: %s\n", repoPath)
		if _, err := git(local, "pull"); err != nil {
			msg := strings.ReplaceAll(strings.TrimSpace(err.Error()), "\n", "\n  ")
			skipped = append(skipped, fmt.Sprintf("%s: %s", repoPath, msg))
			continue
		}
		pulled = append(pulled, repoPath)
	}
	printGroup(out, "pulled", pulled)
	printGroup(out, "skipped", skipped)
	return nil
}
