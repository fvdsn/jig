package jig

import (
	"errors"
	"fmt"
	"strings"
)

type validationResult struct {
	Errors   []string
	Warnings []string
}

func validateDefinition(def *Definition) validationResult {
	var result validationResult
	if def.Version == 1 {
		result.Errors = append(result.Errors, "schema version 1 predates structured references; update refs (see specs: References) and set version: 2")
	} else if def.Version != 2 {
		result.Errors = append(result.Errors, "unsupported or missing version")
	}
	if def.Tree == nil {
		result.Errors = append(result.Errors, "missing tree")
	}
	if def.Source != nil {
		if def.Source.Type != "git" {
			result.Errors = append(result.Errors, "source.type must be git")
		}
		if def.Source.URL == "" {
			result.Errors = append(result.Errors, "source.url is required")
		}
		if def.Source.Path != "" {
			if err := validateSafePath(def.Source.Path); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("invalid source.path: %s", err))
			}
		}
	}

	model, err := flattenDefinition(def)
	if err != nil {
		result.Errors = append(result.Errors, err.Error())
		return result
	}

	// Identities are globally unique across kinds so id references are
	// never ambiguous.
	identities := map[string]string{}
	for _, path := range sortedEntryPaths(&model) {
		entry := model.Entries[path]
		kind := string(entry.Kind)
		if prev, ok := identities[entry.Identity]; ok {
			result.Errors = append(result.Errors, fmt.Sprintf("duplicate identity %s: %s and %s", entry.Identity, prev, path))
		} else {
			identities[entry.Identity] = path
		}
		for _, condition := range entry.Conditions {
			validateCondition(&result, model, path, condition)
		}
		for _, tag := range entry.Tags {
			if tag == "" || strings.ContainsAny(tag, ", \t") {
				result.Errors = append(result.Errors, fmt.Sprintf("%s %s has invalid tag %q: tags must be non-empty without spaces or commas", kind, path, tag))
			}
		}
		for _, key := range sortedMetaKeys(entry.Meta) {
			if key == "" || strings.HasPrefix(key, "$") || strings.ContainsAny(key, "=, \t") {
				result.Errors = append(result.Errors, fmt.Sprintf("%s %s has invalid meta key %q: meta keys must be non-empty without spaces, commas, or \"=\", and must not start with \"$\"", kind, path, key))
			}
		}
		for _, dep := range entry.dependsOn() {
			validateRepoSelector(&result, model, kind+" "+path, "dependency", dep.Ref)
		}
		switch entry.Kind {
		case EntryRepo:
			if entry.Repo.Git == "" {
				result.Errors = append(result.Errors, fmt.Sprintf("repo %s missing git", path))
			}
		case EntryFile:
			validateFileEntry(&result, model, path, entry.File)
		case EntryDir:
			if (len(entry.Dir.Src) == 0) == (entry.Dir.Link == nil) {
				result.Errors = append(result.Errors, fmt.Sprintf("dir %s must define exactly one of src or link", path))
			}
			for _, source := range entry.Dir.Src {
				if _, err := parseDirSrc(source.Src); err != nil {
					result.Errors = append(result.Errors, fmt.Sprintf("dir %s invalid src: %s", path, err))
				}
				if source.OnlyWhen != nil {
					validateCondition(&result, model, path, *source.OnlyWhen)
				}
			}
			if entry.Dir.Link != nil {
				validateLinkRef(&result, model, path, "dir", EntryDir, *entry.Dir.Link)
			}
		}
	}

	for _, cycle := range detectCycles(sortedRepoPaths(&model), repoDependencyPaths(&model)) {
		result.Warnings = append(result.Warnings, "dependency cycle detected: "+strings.Join(cycle, " -> "))
	}
	for _, cycle := range detectCycles(sortedFilePaths(&model), fileLinkPaths(&model)) {
		result.Errors = append(result.Errors, "file link cycle detected: "+strings.Join(cycle, " -> "))
	}
	for _, cycle := range detectCycles(sortedPathsOfKind(&model, EntryDir), dirLinkPaths(&model)) {
		result.Errors = append(result.Errors, "dir link cycle detected: "+strings.Join(cycle, " -> "))
	}
	return result
}

func validateFileEntry(result *validationResult, model Model, path string, file *File) {
	if (len(file.Src) == 0) == (file.Link == nil) {
		result.Errors = append(result.Errors, fmt.Sprintf("file %s must define exactly one of src or link", path))
	}
	for _, source := range file.Src {
		if _, err := parseFileSrc(source.Src); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("file %s invalid src: %s", path, err))
		}
		if source.OnlyWhen != nil {
			validateCondition(result, model, path, *source.OnlyWhen)
		}
	}
	if file.Link != nil {
		validateLinkRef(result, model, path, "file", EntryFile, *file.Link)
	}
	if file.Executable && file.Link != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("file %s cannot use executable with link", path))
	}
}

func validateCondition(result *validationResult, model Model, ownerPath string, condition Condition) {
	validateRepoSelector(result, model, ownerPath, "onlyWhen", condition.Ref)
}

// validateRefSyntax checks the shape shared by all references: exactly one
// selector field, a safe path (before any trailing subtree marker), and
// valid tags. It reports whether the shape is usable at all.
func validateRefSyntax(result *validationResult, owner string, site string, ref Ref) bool {
	if ref.selectorCount() != 1 {
		result.Errors = append(result.Errors, fmt.Sprintf("%s %s must have exactly one of id, path, and tags", owner, site))
		return false
	}
	if ref.Path != "" && ref.Path != "*" {
		base, _ := subtreePath(ref.Path)
		if err := validateSafePath(base); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s has invalid %s path %q: %s", owner, site, ref.Path, err))
			return false
		}
		if strings.Contains(base, "*") {
			result.Errors = append(result.Errors, fmt.Sprintf("%s has invalid %s path %q: \"*\" may only appear as the entire final segment", owner, site, ref.Path))
			return false
		}
	}
	for _, tag := range ref.Tags {
		if tag == "" || strings.ContainsAny(tag, ", \t") {
			result.Errors = append(result.Errors, fmt.Sprintf("%s has invalid %s tag %q: tags must be non-empty without spaces or commas", owner, site, tag))
			return false
		}
	}
	return true
}

// validateRepoSelector checks a dependency or condition reference, whose
// target domain is repositories: an id or exact path must name a declared
// repository, and a subtree or tag selector must match at least one
// (archived included).
func validateRepoSelector(result *validationResult, model Model, owner string, site string, ref Ref) {
	if !validateRefSyntax(result, owner, site, ref) {
		return
	}
	switch {
	case ref.ID != "":
		entries := model.resolveRef(ref)
		if len(entries) == 0 {
			result.Errors = append(result.Errors, fmt.Sprintf("%s %s id %s does not name any entry", owner, site, ref.ID))
		} else if entries[0].Kind != EntryRepo {
			result.Errors = append(result.Errors, fmt.Sprintf("%s %s id %s names a %s, not a repository", owner, site, ref.ID, entries[0].Kind))
		}
	case ref.Path != "":
		if _, subtree := subtreePath(ref.Path); subtree {
			if len(model.resolveRepoRef(ref)) == 0 {
				result.Errors = append(result.Errors, fmt.Sprintf("%s %s %s does not match any repository", owner, site, ref.Path))
			}
			return
		}
		entry, ok := model.Entries[ref.Path]
		switch {
		case !ok && len(model.resolveRepoRef(Ref{Path: ref.Path + "/*"})) > 0:
			result.Errors = append(result.Errors, fmt.Sprintf("%s %s %s does not name a declared entry; use %q for the subtree below it", owner, site, ref.Path, ref.Path+"/*"))
		case !ok:
			result.Errors = append(result.Errors, fmt.Sprintf("%s %s %s does not name any entry", owner, site, ref.Path))
		case entry.Kind == EntryGroup:
			result.Errors = append(result.Errors, fmt.Sprintf("%s %s %s names a group; use %q for its members", owner, site, ref.Path, ref.Path+"/*"))
		case entry.Kind != EntryRepo:
			result.Errors = append(result.Errors, fmt.Sprintf("%s %s %s names a %s, not a repository", owner, site, ref.Path, entry.Kind))
		}
	default:
		if len(model.resolveRepoRef(ref)) == 0 {
			result.Errors = append(result.Errors, fmt.Sprintf("%s %s tags %s do not match any repository", owner, site, strings.Join(ref.Tags, ",")))
		}
	}
}

// validateLinkRef checks a single-target link reference: id or exact path,
// resolving to exactly one other entry of the same kind.
func validateLinkRef(result *validationResult, model Model, path string, kindLabel string, kind EntryKind, ref Ref) {
	owner := kindLabel + " " + path
	if !validateRefSyntax(result, owner, "link", ref) {
		return
	}
	if len(ref.Tags) > 0 {
		result.Errors = append(result.Errors, fmt.Sprintf("%s link cannot use tags; it must name exactly one %s by id or path", owner, kindLabel))
		return
	}
	if _, subtree := subtreePath(ref.Path); ref.Path != "" && subtree {
		result.Errors = append(result.Errors, fmt.Sprintf("%s link %s cannot use \"/*\"; it must name exactly one %s", owner, ref.Path, kindLabel))
		return
	}
	entries := model.resolveRef(ref)
	switch {
	case len(entries) == 0:
		result.Errors = append(result.Errors, fmt.Sprintf("%s link %s does not resolve to any %s", owner, describeRef(ref), kindLabel))
	case entries[0].Kind != kind:
		result.Errors = append(result.Errors, fmt.Sprintf("%s link %s names a %s, not a %s", owner, describeRef(ref), entries[0].Kind, kindLabel))
	case entries[0].Path == path:
		result.Errors = append(result.Errors, fmt.Sprintf("%s cannot link to itself", owner))
	}
}

func (v validationResult) asError(prefix string) error {
	var b strings.Builder
	b.WriteString(prefix)
	for _, msg := range v.Errors {
		b.WriteString("\n  ")
		b.WriteString(msg)
	}
	return errors.New(b.String())
}

// detectCycles reports each cycle reachable in the graph given by neighbors,
// visiting nodes in the given order.
func detectCycles(nodes []string, neighbors func(string) []string) [][]string {
	visited := map[string]int{}
	var stack []string
	var cycles [][]string
	seen := map[string]bool{}

	var visit func(string)
	visit = func(path string) {
		if visited[path] == 2 {
			return
		}
		if visited[path] == 1 {
			idx := indexOf(stack, path)
			if idx >= 0 {
				cycle := append([]string{}, stack[idx:]...)
				cycle = append(cycle, path)
				key := strings.Join(cycle, "\x00")
				if !seen[key] {
					cycles = append(cycles, cycle)
					seen[key] = true
				}
			}
			return
		}
		visited[path] = 1
		stack = append(stack, path)
		for _, next := range neighbors(path) {
			visit(next)
		}
		stack = stack[:len(stack)-1]
		visited[path] = 2
	}

	for _, path := range nodes {
		visit(path)
	}
	return cycles
}

func repoDependencyPaths(model *Model) func(string) []string {
	return func(repoPath string) []string {
		entry, _ := model.entry(repoPath, EntryRepo)
		var paths []string
		for _, dep := range entry.Repo.DependsOn {
			paths = append(paths, entryPaths(model.resolveRepoRef(dep.Ref))...)
		}
		return paths
	}
}

func dirLinkPaths(model *Model) func(string) []string {
	return func(dirPath string) []string {
		if entry, ok := model.entry(dirPath, EntryDir); ok && entry.Dir.linkPath != "" {
			return []string{entry.Dir.linkPath}
		}
		return nil
	}
}

func fileLinkPaths(model *Model) func(string) []string {
	return func(filePath string) []string {
		if entry, ok := model.entry(filePath, EntryFile); ok && entry.File.linkPath != "" {
			return []string{entry.File.linkPath}
		}
		return nil
	}
}

func indexOf(items []string, value string) int {
	for i, item := range items {
		if item == value {
			return i
		}
	}
	return -1
}
