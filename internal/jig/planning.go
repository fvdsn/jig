package jig

// The planner answers one question: given what the user selected and what is
// already installed, which entries does a command operate on?
//
// Repositories become active by exactly three rules:
//
//  R1 Selection: each selected root is active (unless the command only wants
//     the dependencies, as jig deps does).
//  R2 Dependencies: an active repository activates the repositories its
//     dependsOn paths select. Optional dependencies join only when requested
//     or (during sync) already installed; roots never re-enter through a
//     dependency edge; condition-gated targets wait for R3.
//  R3 Conditions: a repository with onlyWhen conditions activates as soon as
//     all of them hold.
//
// Conditions and scopes are judged against the evidence set: the repository
// paths active so far plus the paths of installed repositories defined in
// the schema. Evidence only ever grows, so the rules are monotone and a
// worklist pass reaches the unique fixed point.
//
// Everywhere, archived entries are excluded unless requested or already
// installed. With SkipDeps only R1 applies.
//
// Files and dirs are support artifacts, activated per entry by the first
// matching rule: installed (state records intent), explicit onlyWhen
// conditions hold, or — with no conditions — a repository in the entry's
// scope is in evidence. The scope is the nearest ancestor path containing
// repositories, or the whole workspace. A link entry additionally requires
// its target to be active.

import (
	"fmt"
	"strings"
)

type planOptions struct {
	IncludeOptional          bool
	IncludeInstalledOptional bool
	IncludeArchived          bool
	IncludeRoots             bool
	SkipDeps                 bool // keep the plan to the roots: no dependency or condition expansion
	Installed                map[string]bool
	InstalledFiles           map[string]bool
	InstalledDirs            map[string]bool
}

type plan struct {
	Repos []string
	Files []string
	Dirs  []string
}

func resolvePlan(model *Model, roots []string, opts planOptions) (plan, error) {
	if opts.Installed == nil {
		opts.Installed = map[string]bool{}
	}
	if opts.InstalledFiles == nil {
		opts.InstalledFiles = map[string]bool{}
	}
	if opts.InstalledDirs == nil {
		opts.InstalledDirs = map[string]bool{}
	}
	p := newPlanner(model, opts)
	if err := p.seed(roots); err != nil {
		return plan{}, err
	}
	if !opts.SkipDeps {
		if err := p.solve(roots); err != nil {
			return plan{}, err
		}
	}
	files := artifactsActive(model, EntryFile, p.evidence, opts.InstalledFiles, opts.IncludeArchived)
	dirs := artifactsActive(model, EntryDir, p.evidence, opts.InstalledDirs, opts.IncludeArchived)
	return plan{
		Repos: sortedKeys(p.active),
		Files: orderFilesForApply(model, files),
		Dirs:  orderDirsForApply(model, dirs),
	}, nil
}

// planner carries the repository fixed point of rules R1-R3.
type planner struct {
	model *Model
	opts  planOptions

	repoPaths []string        // all repo paths, sorted
	rootIDs   map[string]bool // root identities never re-enter via dependency edges
	active    map[string]bool // repo paths in the plan
	evidence  map[string]bool // active plus installed defined repo paths
	queue     []string        // repos whose dependencies still need expanding
}

func newPlanner(model *Model, opts planOptions) *planner {
	return &planner{
		model:     model,
		opts:      opts,
		repoPaths: sortedRepoPaths(model),
		rootIDs:   map[string]bool{},
		active:    map[string]bool{},
		evidence:  evidenceSet(model, nil, opts.Installed),
	}
}

// seed applies R1: selected roots enter the plan directly.
func (p *planner) seed(roots []string) error {
	for _, root := range roots {
		entry, ok := p.model.entry(root, EntryRepo)
		if !ok {
			return fmt.Errorf("unknown repository %q", root)
		}
		p.rootIDs[entry.Identity] = true
		if p.opts.IncludeRoots && !archivedExcluded(entry, p.opts.Installed, p.opts.IncludeArchived) {
			p.activate(root)
		}
	}
	return nil
}

func (p *planner) activate(repoPath string) {
	if p.active[repoPath] {
		return
	}
	p.active[repoPath] = true
	p.evidence[repoPath] = true
	p.queue = append(p.queue, repoPath)
}

// solve alternates R2 and R3 until nothing changes. Each repository's
// dependencies expand exactly once (when it activates); condition-gated
// entries are re-checked whenever the evidence has grown.
func (p *planner) solve(roots []string) error {
	if !p.opts.IncludeRoots {
		// jig deps: the roots stay out of the plan, but their dependencies
		// seed it.
		p.queue = append(p.queue, roots...)
	}
	for {
		for len(p.queue) > 0 {
			repoPath := p.queue[0]
			p.queue = p.queue[1:]
			if err := p.expandDependencies(repoPath); err != nil {
				return err
			}
		}
		if !p.activateConditionalRepos() {
			return nil
		}
	}
}

// expandDependencies applies R2 for one repository.
func (p *planner) expandDependencies(repoPath string) error {
	entry, _ := p.model.entry(repoPath, EntryRepo)
	for _, dep := range entry.Repo.DependsOn {
		selection, err := p.model.Select(NodeQuery{Path: dep.Path, IncludeArchived: true})
		if err != nil {
			return fmt.Errorf("invalid dependency %s for %s: %w", dep.Path, repoPath, err)
		}
		matches := selection.ofKind(EntryRepo)
		if len(matches) == 0 {
			return fmt.Errorf("dependency %s for %s does not resolve to any repository", dep.Path, repoPath)
		}
		for _, match := range matches {
			if archivedExcluded(match, p.opts.Installed, p.opts.IncludeArchived) {
				continue
			}
			if dep.Optional && !p.opts.IncludeOptional && !(p.opts.IncludeInstalledOptional && p.opts.Installed[match.Identity]) {
				continue
			}
			if p.rootIDs[match.Identity] {
				continue
			}
			if len(match.Conditions) > 0 && !conditionsMetIn(p.model, p.evidence, match.Conditions) {
				continue // R3 picks it up if its conditions come to hold
			}
			p.activate(match.Path)
		}
	}
	return nil
}

// activateConditionalRepos applies R3 once, reporting whether anything new
// became active.
func (p *planner) activateConditionalRepos() bool {
	activated := false
	for _, repoPath := range p.repoPaths {
		if p.active[repoPath] {
			continue
		}
		entry, _ := p.model.entry(repoPath, EntryRepo)
		if len(entry.Conditions) == 0 {
			continue
		}
		if archivedExcluded(entry, p.opts.Installed, p.opts.IncludeArchived) {
			continue
		}
		if conditionsMetIn(p.model, p.evidence, entry.Conditions) {
			p.activate(repoPath)
			activated = true
		}
	}
	return activated
}

// evidenceSet builds the repository paths that conditions and scopes are
// judged against: activeRepos (path-keyed) plus the schema paths of the
// installed identities.
func evidenceSet(model *Model, activeRepos map[string]bool, installedIdentities map[string]bool) map[string]bool {
	evidence := map[string]bool{}
	for repoPath := range activeRepos {
		evidence[repoPath] = true
	}
	if len(installedIdentities) > 0 {
		identityToPath := repoIdentityToPath(model)
		for identity := range installedIdentities {
			if repoPath, ok := identityToPath[identity]; ok {
				evidence[repoPath] = true
			}
		}
	}
	return evidence
}

func conditionsMetIn(model *Model, evidence map[string]bool, conditions []Condition) bool {
	for _, condition := range conditions {
		if !conditionMetIn(model, evidence, condition) {
			return false
		}
	}
	return true
}

// conditionMetIn reports whether some repository in the evidence satisfies
// the condition: under its path (when given) and carrying all its tags
// (when given).
func conditionMetIn(model *Model, evidence map[string]bool, condition Condition) bool {
	for repoPath := range evidence {
		if condition.Path != "" && !pathMatches(condition.Path, repoPath) {
			continue
		}
		if len(condition.Tags) > 0 {
			entry, ok := model.entry(repoPath, EntryRepo)
			if !ok || !entry.hasAllTags(condition.Tags) {
				continue
			}
		}
		return true
	}
	return false
}

// conditionMatches reports whether a single condition holds against active
// and installed repositories; used by per-source dir gating.
func conditionMatches(condition Condition, activeRepos map[string]bool, installedIdentities map[string]bool, model *Model) bool {
	return conditionMetIn(model, evidenceSet(model, activeRepos, installedIdentities), condition)
}

// artifactsActive computes the active files or dirs for the given repository
// evidence. Link chains are resolved by a memoized walk: a link entry is
// active only when its whole chain is; cycles (rejected by validation)
// resolve to inactive.
func artifactsActive(model *Model, kind EntryKind, evidence map[string]bool, installedSelf map[string]bool, includeArchived bool) map[string]bool {
	repoPaths := sortedRepoPaths(model)
	linkOf := func(entry Entry) string {
		if kind == EntryFile {
			return entry.File.Link
		}
		return entry.Dir.Link
	}

	const (
		unknown = iota
		visiting
		activeState
		inactiveState
	)
	state := map[string]int{}
	var isActive func(path string) bool
	isActive = func(path string) bool {
		switch state[path] {
		case activeState:
			return true
		case inactiveState, visiting:
			return false
		}
		entry, ok := model.entry(path, kind)
		if !ok {
			return false
		}
		state[path] = visiting
		result := !archivedExcluded(entry, installedSelf, includeArchived) &&
			artifactBaseActive(model, repoPaths, entry, evidence, installedSelf)
		if result {
			if target := linkOf(entry); target != "" {
				result = isActive(target)
			}
		}
		if result {
			state[path] = activeState
		} else {
			state[path] = inactiveState
		}
		return result
	}

	set := map[string]bool{}
	for _, path := range sortedPathsOfKind(model, kind) {
		if isActive(path) {
			set[path] = true
		}
	}
	return set
}

// artifactBaseActive is the per-entry activation rule for files and dirs:
// installed, explicit conditions hold, or a repository in scope is in
// evidence.
func artifactBaseActive(model *Model, repoPaths []string, entry Entry, evidence map[string]bool, installedSelf map[string]bool) bool {
	if installedSelf[entry.Identity] {
		return true
	}
	if len(entry.Conditions) > 0 {
		return conditionsMetIn(model, evidence, entry.Conditions)
	}
	scope := entryScope(repoPaths, entry.Path)
	if scope == "" {
		return len(evidence) > 0
	}
	return conditionMetIn(model, evidence, Condition{Path: scope})
}

// entryScope returns the nearest ancestor path of entryPath containing at
// least one repository, or "" for the workspace root.
func entryScope(repoPaths []string, entryPath string) string {
	for scope := parentPath(entryPath); scope != ""; scope = parentPath(scope) {
		for _, repoPath := range repoPaths {
			if pathMatches(scope, repoPath) {
				return scope
			}
		}
	}
	return ""
}

func parentPath(path string) string {
	idx := strings.LastIndex(path, "/")
	if idx < 0 {
		return ""
	}
	return path[:idx]
}

// activeFilesForRepoSet reports the active files given active repository
// paths and installed repository identities; status uses it directly.
func activeFilesForRepoSet(model *Model, activeRepos map[string]bool, installedRepos map[string]bool, installedFiles map[string]bool, includeArchived bool) map[string]bool {
	return artifactsActive(model, EntryFile, evidenceSet(model, activeRepos, installedRepos), installedFiles, includeArchived)
}

// activeDirsForRepoSet mirrors activeFilesForRepoSet for $dir entries.
func activeDirsForRepoSet(model *Model, activeRepos map[string]bool, installedRepos map[string]bool, installedDirs map[string]bool, includeArchived bool) map[string]bool {
	return artifactsActive(model, EntryDir, evidenceSet(model, activeRepos, installedRepos), installedDirs, includeArchived)
}

// archivedExcluded reports whether an archived entry should be left out of a
// plan: archived entries stay in only when requested or already installed.
func archivedExcluded(entry Entry, installed map[string]bool, includeArchived bool) bool {
	return entry.archived() && !includeArchived && !installed[entry.Identity]
}

func orderFilesForApply(model *Model, active map[string]bool) []string {
	return orderLinkedForApply(active, func(path string) string {
		if entry, ok := model.entry(path, EntryFile); ok {
			return entry.File.Link
		}
		return ""
	})
}

func orderDirsForApply(model *Model, active map[string]bool) []string {
	return orderLinkedForApply(active, func(path string) string {
		if entry, ok := model.entry(path, EntryDir); ok {
			return entry.Dir.Link
		}
		return ""
	})
}

// orderLinkedForApply orders active entries so link targets are applied
// before the links pointing at them.
func orderLinkedForApply(active map[string]bool, linkOf func(string) string) []string {
	visited := map[string]bool{}
	visiting := map[string]bool{}
	var result []string
	var visit func(string)
	visit = func(path string) {
		if visited[path] || !active[path] {
			return
		}
		if visiting[path] {
			return
		}
		visiting[path] = true
		if target := linkOf(path); target != "" {
			visit(target)
		}
		visiting[path] = false
		visited[path] = true
		result = append(result, path)
	}
	for _, path := range sortedKeys(active) {
		visit(path)
	}
	return result
}
