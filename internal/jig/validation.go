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
	if def.Version != 1 {
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

	identities := map[EntryKind]map[string]string{}
	for _, path := range sortedEntryPaths(&model) {
		entry := model.Entries[path]
		kind := string(entry.Kind)
		if identities[entry.Kind] == nil {
			identities[entry.Kind] = map[string]string{}
		}
		if prev, ok := identities[entry.Kind][entry.Identity]; ok {
			result.Errors = append(result.Errors, fmt.Sprintf("duplicate %s identity %s: %s and %s", kind, entry.Identity, prev, path))
		} else {
			identities[entry.Kind][entry.Identity] = path
		}
		for _, condition := range entry.Conditions {
			validateCondition(&result, model, path, condition)
		}
		for _, tag := range entry.Tags {
			if tag == "" || strings.ContainsAny(tag, ", \t") {
				result.Errors = append(result.Errors, fmt.Sprintf("%s %s has invalid tag %q: tags must be non-empty without spaces or commas", kind, path, tag))
			}
		}
		for _, dep := range entry.dependsOn() {
			if err := validateSafePath(dep.Path); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("%s %s has invalid dependency path %q: %s", kind, path, dep.Path, err))
				continue
			}
			matches, _ := model.Select(NodeQuery{Path: dep.Path, IncludeArchived: true})
			if len(matches.ofKind(EntryRepo)) == 0 {
				result.Errors = append(result.Errors, fmt.Sprintf("%s %s dependency %s does not resolve to any repository", kind, path, dep.Path))
			}
		}
		switch entry.Kind {
		case EntryRepo:
			if entry.Repo.Git == "" {
				result.Errors = append(result.Errors, fmt.Sprintf("repo %s missing git", path))
			}
		case EntryFile:
			validateFileEntry(&result, model, path, entry.File)
		case EntryDir:
			if len(entry.Dir.Src) == 0 {
				result.Errors = append(result.Errors, fmt.Sprintf("dir %s missing src", path))
			}
			for _, source := range entry.Dir.Src {
				if _, err := parseDirSrc(source.Src); err != nil {
					result.Errors = append(result.Errors, fmt.Sprintf("dir %s invalid src: %s", path, err))
				}
				if source.OnlyWhen != nil {
					validateCondition(&result, model, path, *source.OnlyWhen)
				}
			}
		}
	}

	for _, cycle := range detectCycles(sortedRepoPaths(&model), repoDependencyPaths(&model)) {
		result.Warnings = append(result.Warnings, "dependency cycle detected: "+strings.Join(cycle, " -> "))
	}
	for _, cycle := range detectCycles(sortedFilePaths(&model), fileLinkPaths(&model)) {
		result.Errors = append(result.Errors, "file link cycle detected: "+strings.Join(cycle, " -> "))
	}
	return result
}

func validateFileEntry(result *validationResult, model Model, path string, file *File) {
	if (file.Src == "") == (file.Link == "") {
		result.Errors = append(result.Errors, fmt.Sprintf("file %s must define exactly one of src or link", path))
	}
	if file.Src != "" {
		if _, err := parseFileSrc(file.Src); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("file %s invalid src: %s", path, err))
		}
	}
	if file.Link != "" {
		if err := validateSafePath(file.Link); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("file %s invalid link: %s", path, err))
		} else if _, ok := model.entry(file.Link, EntryFile); !ok {
			result.Errors = append(result.Errors, fmt.Sprintf("file %s link %s does not resolve to any file", path, file.Link))
		} else if file.Link == path {
			result.Errors = append(result.Errors, fmt.Sprintf("file %s cannot link to itself", path))
		}
	}
	if file.Executable && file.Link != "" {
		result.Errors = append(result.Errors, fmt.Sprintf("file %s cannot use executable with link", path))
	}
}

func validateCondition(result *validationResult, model Model, ownerPath string, condition Condition) {
	if condition.Path == "" {
		result.Errors = append(result.Errors, fmt.Sprintf("%s has onlyWhen with empty path", ownerPath))
		return
	}
	if err := validateSafePath(condition.Path); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("%s has invalid onlyWhen path %q: %s", ownerPath, condition.Path, err))
		return
	}
	matches, _ := model.Select(NodeQuery{Path: condition.Path, IncludeArchived: true})
	if len(matches.ofKind(EntryRepo)) == 0 {
		result.Errors = append(result.Errors, fmt.Sprintf("%s onlyWhen path %s does not resolve to any repository", ownerPath, condition.Path))
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
			selection, err := model.Select(NodeQuery{Path: dep.Path, IncludeArchived: true})
			if err != nil {
				continue
			}
			paths = append(paths, entryPaths(selection.ofKind(EntryRepo))...)
		}
		return paths
	}
}

func fileLinkPaths(model *Model) func(string) []string {
	return func(filePath string) []string {
		if entry, ok := model.entry(filePath, EntryFile); ok && entry.File.Link != "" {
			return []string{entry.File.Link}
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
