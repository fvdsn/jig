package jig

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

type Model struct {
	Repos  map[string]RepoEntry
	Files  map[string]FileEntry
	Groups map[string]GroupEntry
}

type RepoEntry struct {
	Path       string
	Identity   string
	Repo       Repo
	Conditions []Condition
}

type FileEntry struct {
	Path       string
	Identity   string
	File       File
	Conditions []Condition
}

type GroupEntry struct {
	Path       string
	Group      Group
	Conditions []Condition
}

type inheritedGroup struct {
	Description string
	Web         string
	Archived    bool
	DependsOn   []Dependency
	Conditions  []Condition
}

func flattenDefinition(def *Definition) (Model, error) {
	model := Model{Repos: map[string]RepoEntry{}, Files: map[string]FileEntry{}, Groups: map[string]GroupEntry{}}
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
			identity := repo.ID
			if identity == "" {
				identity = path
			}
			repo = applyInheritedRepo(repo, inherited)
			conditions := append([]Condition{}, inherited.Conditions...)
			if repo.OnlyWhen != nil {
				conditions = append(conditions, *repo.OnlyWhen)
			}
			model.Repos[path] = RepoEntry{Path: path, Identity: identity, Repo: repo, Conditions: conditions}
			return nil
		}
		var file File
		if err := json.Unmarshal(obj["$file"], &file); err != nil {
			return fmt.Errorf("invalid $file at %s: %s", path, err)
		}
		identity := file.ID
		if identity == "" {
			identity = path
		}
		file = applyInheritedFile(file, inherited)
		conditions := append([]Condition{}, inherited.Conditions...)
		if file.OnlyWhen != nil {
			conditions = append(conditions, *file.OnlyWhen)
		}
		model.Files[path] = FileEntry{Path: path, Identity: identity, File: file, Conditions: conditions}
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
		model.Groups[path] = GroupEntry{Path: path, Group: group, Conditions: append([]Condition{}, inherited.Conditions...)}
	}
	return flattenTreeMap(obj, path, inherited, model)
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
