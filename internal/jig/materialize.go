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
	var mu sync.Mutex
	forEachParallel(len(plan.Repos), func(i int) {
		mu.Lock()
		stateRepo, hasState := ws.State.Repos[entries[i].Identity]
		mu.Unlock()
		result := ensureRepo(ws.Root, entries[i], stateRepo, hasState, opts.Sync)
		mu.Lock()
		defer mu.Unlock()
		if result.Remove {
			delete(ws.State.Repos, entries[i].Identity)
		}
		if result.StateRepo != nil {
			ws.State.Repos[entries[i].Identity] = *result.StateRepo
		}
		for _, message := range result.Messages {
			fmt.Fprintln(out, message)
		}
		if result.Err != nil {
			skip(plan.Repos[i], result.Err)
		}
	})
	fetcher := newFileFetcher()
	activeRepos := map[string]bool{}
	for _, repoPath := range plan.Repos {
		activeRepos[repoPath] = true
	}
	fetcher.prefetchMirrors(activeSourceURLs(&ws.Model, plan, activeRepos, installedRepos))
	for _, filePath := range plan.Files {
		if err := ensureFile(out, ws.Root, &ws.Model, &ws.State, filePath, opts.Sync, fetcher, activeRepos, installedRepos); err != nil {
			skip(filePath, err)
		}
	}
	for _, dirPath := range plan.Dirs {
		if err := ensureDir(out, ws.Root, &ws.Model, &ws.State, dirPath, opts.Sync, fetcher, activeRepos, installedRepos); err != nil {
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
func activeSourceURLs(model *Model, plan plan, activeRepos map[string]bool, installedRepos map[string]bool) []string {
	urls := map[string]bool{}
	add := func(sources SrcList, parse func(string) (fileSrc, error)) {
		for _, source := range sources {
			if source.OnlyWhen != nil && !conditionMatches(*source.OnlyWhen, activeRepos, installedRepos, model) {
				continue
			}
			if parsed, err := parse(source.Src); err == nil {
				urls[parsed.GitURL] = true
			}
		}
	}
	for _, filePath := range plan.Files {
		if entry, ok := model.entry(filePath, EntryFile); ok && entry.File.Link == nil {
			add(effectiveFileSrc(model, entry.File), parseFileSrc)
		}
	}
	for _, dirPath := range plan.Dirs {
		if entry, ok := model.entry(dirPath, EntryDir); ok && entry.Dir.Link == nil {
			add(effectiveDirSrc(model, entry.Dir), parseDirSrc)
		}
	}
	return sortedKeys(urls)
}

// effectiveFileSrc returns the sources a file entry materializes from: its
// own for a src entry, the target's for a copy entry.
func effectiveFileSrc(model *Model, file *File) SrcList {
	if file.Copy != nil {
		if target, ok := model.entry(file.copyPath, EntryFile); ok {
			return target.File.Src
		}
		return nil
	}
	return file.Src
}

// effectiveDirSrc mirrors effectiveFileSrc for $dir entries.
func effectiveDirSrc(model *Model, dir *Dir) SrcList {
	if dir.Copy != nil {
		if target, ok := model.entry(dir.copyPath, EntryDir); ok {
			return target.Dir.Src
		}
		return nil
	}
	return dir.Src
}
