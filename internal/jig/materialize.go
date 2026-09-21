package jig

import (
	"fmt"
	"io"
	"sync"
)

type applyOptions struct {
	IncludeOptional bool
	IncludeArchived bool
	SkipDeps        bool // materialize only the roots, without their dependencies
	Sync            bool // keep installed optional deps and allow moving installed entries
}

// resolveAndApplyPlan expands roots into a full plan and materializes it.
func resolveAndApplyPlan(out io.Writer, ws *Workspace, roots []string, explicitFiles []string, explicitDirs []string, opts applyOptions) error {
	installed := ws.installedNodes()
	plan, err := resolvePlan(&ws.Model, roots, planOptions{
		IncludeOptional:          opts.IncludeOptional,
		IncludeInstalledOptional: opts.Sync,
		IncludeArchived:          opts.IncludeArchived,
		IncludeRoots:             true,
		SkipDeps:                 opts.SkipDeps,
		Installed:                installed.Repos,
		InstalledFiles:           installed.Files,
		InstalledDirs:            installed.Dirs,
	})
	if err != nil {
		return err
	}
	plan = includeExplicitArtifacts(&ws.Model, plan, EntryFile, explicitFiles)
	plan = includeExplicitArtifacts(&ws.Model, plan, EntryDir, explicitDirs)
	if !opts.IncludeArchived {
		plan = excludeArchivedArtifacts(&ws.Model, plan, EntryFile, installed.Files)
		plan = excludeArchivedArtifacts(&ws.Model, plan, EntryDir, installed.Dirs)
	}
	err = applyPlan(out, ws, plan, opts, installed.Repos)
	ws.invalidateInstalled()
	return err
}

// artifacts returns the plan's files or dirs.
func (p plan) artifacts(kind EntryKind) []string {
	if kind == EntryFile {
		return p.Files
	}
	return p.Dirs
}

// withArtifacts returns the plan with its files or dirs replaced.
func (p plan) withArtifacts(kind EntryKind, paths []string) plan {
	if kind == EntryFile {
		p.Files = paths
	} else {
		p.Dirs = paths
	}
	return p
}

// includeExplicitArtifacts adds explicitly selected files or dirs to the
// plan along with the link and copy targets they need.
func includeExplicitArtifacts(model *Model, base plan, kind EntryKind, paths []string) plan {
	active := map[string]bool{}
	for _, path := range base.artifacts(kind) {
		active[path] = true
	}
	var add func(string)
	add = func(path string) {
		entry, ok := model.entry(path, kind)
		if !ok || active[path] {
			return
		}
		active[path] = true
		if target := entry.targetPath(); target != "" {
			add(target)
		}
	}
	for _, path := range paths {
		add(path)
	}
	return base.withArtifacts(kind, orderArtifactsForApply(model, kind, active))
}

// excludeArchivedArtifacts drops uninstalled archived files or dirs from
// the plan, and with them any link or copy whose target dropped out.
func excludeArchivedArtifacts(model *Model, base plan, kind EntryKind, installed map[string]bool) plan {
	active := map[string]bool{}
	for _, path := range base.artifacts(kind) {
		entry, ok := model.entry(path, kind)
		if ok && !archivedExcluded(entry, installed, false) {
			active[path] = true
		}
	}
	changed := true
	for changed {
		changed = false
		for path := range active {
			entry, _ := model.entry(path, kind)
			if target := entry.targetPath(); target != "" && !active[target] {
				delete(active, path)
				changed = true
			}
		}
	}
	return base.withArtifacts(kind, orderArtifactsForApply(model, kind, active))
}

// applyPlan materializes the plan. An entry that cannot be brought to its
// desired state is announced as skipped where it would have reported, then
// listed with its reason in a skipped group at the end; anything skipped
// makes the command fail, so scripts and agents see partial failures in
// the exit code.
func applyPlan(out io.Writer, ws *Workspace, plan plan, opts applyOptions, installedRepos map[string]bool) error {
	var skipped []string
	skip := func(path string, err error) {
		skipped = append(skipped, fmt.Sprintf("%s: %s", path, err))
		fmt.Fprintf(out, "skipped: %s\n", path)
	}

	// Repositories are independent of each other, so the git work runs in
	// parallel; each result is applied to state and printed as it completes,
	// so long runs show progress instead of a report at the end.
	entries := make([]Entry, len(plan.Repos))
	for i, repoPath := range plan.Repos {
		entries[i], _ = ws.Model.entry(repoPath, EntryRepo)
	}
	// A clone can run for minutes with nothing to report, so a transient
	// status line names the repositories in flight, like the git verbs do.
	tracker := newProgress(len(plan.Repos))
	var mu sync.Mutex
	forEachParallel(len(plan.Repos), func(i int) {
		mu.Lock()
		stateRepo, hasState := ws.State.Repos[entries[i].Identity]
		mu.Unlock()
		tracker.start(plan.Repos[i])
		result := ensureRepo(ws.Root, entries[i], stateRepo, hasState, opts.Sync)
		tracker.finish(plan.Repos[i])
		mu.Lock()
		defer mu.Unlock()
		if result.Remove {
			delete(ws.State.Repos, entries[i].Identity)
		}
		if result.StateRepo != nil {
			ws.State.Repos[entries[i].Identity] = *result.StateRepo
		}
		for _, message := range result.Messages {
			tracker.println(out, message)
		}
		if result.Err != nil {
			skipped = append(skipped, fmt.Sprintf("%s: %s", plan.Repos[i], result.Err))
			tracker.println(out, "skipped: "+plan.Repos[i])
		}
	})
	tracker.close()
	activeRepos := map[string]bool{}
	for _, repoPath := range plan.Repos {
		activeRepos[repoPath] = true
	}
	m := newMaterializer(out, ws.Root, &ws.Model, &ws.State, newFileFetcher(), activeRepos, installedRepos, opts.Sync)
	m.fetcher.prefetchMirrors(activeSourceURLs(m.model, plan, m.evidence))
	for _, filePath := range plan.Files {
		if err := m.ensureFile(filePath); err != nil {
			skip(filePath, err)
		}
	}
	for _, dirPath := range plan.Dirs {
		if err := m.ensureDir(dirPath); err != nil {
			skip(dirPath, err)
		}
	}
	printGroup(out, "skipped", skipped)
	if len(skipped) > 0 {
		return fmt.Errorf("%d entries skipped", len(skipped))
	}
	return nil
}

// activeSourceURLs collects the distinct source repository URLs the plan's
// file and dir entries are about to fetch, honoring per-source conditions,
// so their mirrors can be freshened in parallel up front.
func activeSourceURLs(model *Model, plan plan, evidence map[string]bool) []string {
	urls := map[string]bool{}
	add := func(kind EntryKind, paths []string, parse func(string) (fileSrc, error)) {
		for _, path := range paths {
			entry, ok := model.entry(path, kind)
			if !ok || entry.isLink() {
				continue
			}
			sources, err := effectiveSources(model, entry)
			if err != nil {
				continue
			}
			for _, source := range sources {
				if source.OnlyWhen != nil && !conditionMetIn(model, evidence, *source.OnlyWhen) {
					continue
				}
				if parsed, err := parse(source.Src); err == nil {
					urls[parsed.GitURL] = true
				}
			}
		}
	}
	add(EntryFile, plan.Files, parseFileSrc)
	add(EntryDir, plan.Dirs, parseDirSrc)
	return sortedKeys(urls)
}
