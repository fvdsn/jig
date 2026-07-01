package jig

import (
	"fmt"
	"io"
	"strings"
	"sync"
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

	type candidate struct {
		repoPath string
		local    string
	}
	var candidates []candidate
	for _, entry := range selection.ofKind(EntryRepo) {
		if local, ok := installedPath(ws.Root, &ws.Model, &ws.State, entry.Path); ok {
			candidates = append(candidates, candidate{entry.Path, local})
		}
	}

	var mu sync.Mutex
	var skipped []string
	forEachParallel(len(candidates), func(i int) {
		if _, err := git(candidates[i].local, "pull", "--ff-only"); err != nil {
			msg := strings.ReplaceAll(strings.TrimSpace(err.Error()), "\n", "\n  ")
			mu.Lock()
			skipped = append(skipped, fmt.Sprintf("%s: %s", candidates[i].repoPath, msg))
			mu.Unlock()
			return
		}
		mu.Lock()
		fmt.Fprintf(out, "pulled: %s\n", candidates[i].repoPath)
		mu.Unlock()
	})
	printGroup(out, "skipped", skipped)
	return nil
}
