package jig

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

type Model struct {
	Entries map[string]Entry
}

type EntryKind string

const (
	EntryRepo  EntryKind = "repo"
	EntryFile  EntryKind = "file"
	EntryGroup EntryKind = "group"
)

type Entry struct {
	Path       string
	Identity   string
	Kind       EntryKind
	Conditions []Condition

	Repo  *Repo
	File  *File
	Group *Group
}

type inheritedGroup struct {
	Description string
	Web         string
	Archived    bool
	DependsOn   []Dependency
	Conditions  []Condition
}

func flattenDefinition(def *Definition) (Model, error) {
	model := Model{Entries: map[string]Entry{}}
	if def.Tree == nil {
		return model, errors.New("missing tree")
	}
	if err := flattenTreeMap(def.Tree, "", inheritedGroup{}, &model); err != nil {
		return model, err
	}
	return model, nil
}

func flattenTreeMap(nodes map[string]json.RawMessage, prefix string, inherited inheritedGroup, model *Model) error {
	keys := make([]string, 0, len(nodes))
	for key := range nodes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if strings.HasPrefix(key, "$") {
			return fmt.Errorf("reserved tree key %q cannot be used as a path segment", key)
		}
		path, err := joinTreePath(prefix, key)
		if err != nil {
			return err
		}
		if err := flattenTreeNode(path, nodes[key], inherited, model); err != nil {
			return err
		}
	}
	return nil
}

func flattenTreeNode(path string, raw json.RawMessage, inherited inheritedGroup, model *Model) error {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return fmt.Errorf("invalid tree node %s: %s", path, err)
	}
	_, hasRepo := obj["$repo"]
	_, hasFile := obj["$file"]
	groupRaw, hasGroup := obj["$group"]
	if hasRepo && hasFile {
		return fmt.Errorf("tree node %s cannot contain both $repo and $file", path)
	}
	if hasRepo || hasFile {
		if len(obj) != 1 {
			return fmt.Errorf("tree node %s cannot contain child nodes with $repo or $file", path)
		}
		if err := validateSafePath(path); err != nil {
			return fmt.Errorf("invalid tree path %q: %s", path, err)
		}
		if hasRepo {
			var repo Repo
			if err := json.Unmarshal(obj["$repo"], &repo); err != nil {
				return fmt.Errorf("invalid $repo at %s: %s", path, err)
			}
			repo = applyInheritedRepo(repo, inherited)
			model.Entries[path] = Entry{
				Path:       path,
				Identity:   identityOr(repo.ID, path),
				Kind:       EntryRepo,
				Conditions: leafConditions(inherited, repo.OnlyWhen),
				Repo:       &repo,
			}
			return nil
		}
		var file File
		if err := json.Unmarshal(obj["$file"], &file); err != nil {
			return fmt.Errorf("invalid $file at %s: %s", path, err)
		}
		file = applyInheritedFile(file, inherited)
		model.Entries[path] = Entry{
			Path:       path,
			Identity:   identityOr(file.ID, path),
			Kind:       EntryFile,
			Conditions: leafConditions(inherited, file.OnlyWhen),
			File:       &file,
		}
		return nil
	}
	if hasGroup {
		var group Group
		if err := json.Unmarshal(groupRaw, &group); err != nil {
			return fmt.Errorf("invalid $group at %s: %s", path, err)
		}
		delete(obj, "$group")
		inherited = mergeGroup(inherited, group)
		if inherited.Archived {
			group.Archived = true
		}
		model.Entries[path] = Entry{
			Path:       path,
			Identity:   identityOr(group.ID, path),
			Kind:       EntryGroup,
			Conditions: append([]Condition{}, inherited.Conditions...),
			Group:      &group,
		}
	}
	return flattenTreeMap(obj, path, inherited, model)
}

func identityOr(id string, path string) string {
	if id == "" {
		return path
	}
	return id
}

func leafConditions(inherited inheritedGroup, onlyWhen *Condition) []Condition {
	conditions := append([]Condition{}, inherited.Conditions...)
	if onlyWhen != nil {
		conditions = append(conditions, *onlyWhen)
	}
	return conditions
}

func applyInheritedRepo(repo Repo, inherited inheritedGroup) Repo {
	if repo.Description == "" {
		repo.Description = inherited.Description
	}
	if repo.Web == "" {
		repo.Web = inherited.Web
	}
	if inherited.Archived {
		repo.Archived = true
	}
	if len(inherited.DependsOn) > 0 {
		deps := append([]Dependency{}, inherited.DependsOn...)
		repo.DependsOn = append(deps, repo.DependsOn...)
	}
	return repo
}

func applyInheritedFile(file File, inherited inheritedGroup) File {
	if file.Description == "" {
		file.Description = inherited.Description
	}
	if inherited.Archived {
		file.Archived = true
	}
	return file
}

func mergeGroup(inherited inheritedGroup, group Group) inheritedGroup {
	merged := inheritedGroup{
		Description: inherited.Description,
		Web:         inherited.Web,
		Archived:    inherited.Archived,
		DependsOn:   append([]Dependency{}, inherited.DependsOn...),
		Conditions:  append([]Condition{}, inherited.Conditions...),
	}
	if group.Description != "" {
		merged.Description = group.Description
	}
	if group.Web != "" {
		merged.Web = group.Web
	}
	if group.Archived {
		merged.Archived = true
	}
	if len(group.DependsOn) > 0 {
		merged.DependsOn = append(merged.DependsOn, group.DependsOn...)
	}
	if group.OnlyWhen != nil {
		merged.Conditions = append(merged.Conditions, *group.OnlyWhen)
	}
	return merged
}

func joinTreePath(prefix, key string) (string, error) {
	if err := validateSafePath(key); err != nil {
		return "", fmt.Errorf("invalid tree key %q: %s", key, err)
	}
	if prefix == "" {
		return key, nil
	}
	return prefix + "/" + key, nil
}
