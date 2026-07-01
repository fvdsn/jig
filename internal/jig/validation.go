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
	if len(def.Repos) > 0 {
		result.Errors = append(result.Errors, "legacy repos field is not supported; use tree with $repo nodes")
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

	repoIDs := map[string]string{}
	for _, path := range sortedRepoPaths(&model) {
		entry, _ := model.entry(path, EntryRepo)
		if entry.Repo.Git == "" {
			result.Errors = append(result.Errors, fmt.Sprintf("repository %s missing git", path))
		}
		if prev, ok := repoIDs[entry.Identity]; ok {
			result.Errors = append(result.Errors, fmt.Sprintf("duplicate repository identity %s: %s and %s", entry.Identity, prev, path))
		} else {
			repoIDs[entry.Identity] = path
		}
		for _, condition := range entry.Conditions {
			validateCondition(&result, model, path, condition)
		}
		for _, dep := range entry.Repo.DependsOn {
			if err := validateSafePath(dep.Path); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("repository %s has invalid dependency path %q: %s", path, dep.Path, err))
				continue
			}
			matches, _ := model.Select(NodeQuery{Path: dep.Path, IncludeArchived: true})
			if len(matches.ofKind(EntryRepo)) == 0 {
				result.Errors = append(result.Errors, fmt.Sprintf("repository %s dependency %s does not resolve to any repository", path, dep.Path))
			}
		}
	}

	fileIDs := map[string]string{}
	for _, path := range sortedFilePaths(&model) {
		entry, _ := model.entry(path, EntryFile)
		if (entry.File.Src == "") == (entry.File.Link == "") {
			result.Errors = append(result.Errors, fmt.Sprintf("file %s must define exactly one of src or link", path))
		}
		if entry.File.Src != "" {
			if _, err := parseFileSrc(entry.File.Src); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("file %s invalid src: %s", path, err))
			}
		}
		if entry.File.Link != "" {
			if err := validateSafePath(entry.File.Link); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("file %s invalid link: %s", path, err))
			} else if _, ok := model.entry(entry.File.Link, EntryFile); !ok {
				result.Errors = append(result.Errors, fmt.Sprintf("file %s link %s does not resolve to any file", path, entry.File.Link))
			} else if entry.File.Link == path {
				result.Errors = append(result.Errors, fmt.Sprintf("file %s cannot link to itself", path))
			}
		}
		if entry.File.Executable && entry.File.Link != "" {
			result.Errors = append(result.Errors, fmt.Sprintf("file %s cannot use executable with link", path))
		}
		if prev, ok := fileIDs[entry.Identity]; ok {
			result.Errors = append(result.Errors, fmt.Sprintf("duplicate file identity %s: %s and %s", entry.Identity, prev, path))
		} else {
			fileIDs[entry.Identity] = path
		}
		for _, condition := range entry.Conditions {
			validateCondition(&result, model, path, condition)
		}
	}

	groupIDs := map[string]string{}
	for _, path := range sortedGroupPaths(&model) {
		entry, _ := model.entry(path, EntryGroup)
		if prev, ok := groupIDs[entry.Identity]; ok {
			result.Errors = append(result.Errors, fmt.Sprintf("duplicate group identity %s: %s and %s", entry.Identity, prev, path))
		} else {
			groupIDs[entry.Identity] = path
		}
		for _, dep := range entry.Group.DependsOn {
			if err := validateSafePath(dep.Path); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("group %s has invalid dependency path %q: %s", path, dep.Path, err))
				continue
			}
			matches, _ := model.Select(NodeQuery{Path: dep.Path, IncludeArchived: true})
			if len(matches.ofKind(EntryRepo)) == 0 {
				result.Errors = append(result.Errors, fmt.Sprintf("group %s dependency %s does not resolve to any repository", path, dep.Path))
			}
		}
		for _, condition := range entry.Conditions {
			validateCondition(&result, model, path, condition)
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
