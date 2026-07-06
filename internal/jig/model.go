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
	EntryDir   EntryKind = "dir"
	EntryGroup EntryKind = "group"
)

type Entry struct {
	Path       string
	Identity   string
	Kind       EntryKind
	Conditions []Condition
	Tags       []string // declared tags plus tags inherited from parent groups

	Repo  *Repo
	File  *File
	Dir   *Dir
	Group *Group
}

type inheritedGroup struct {
	Description string
	Web         string
	Archived    bool
	Tags        []string
	DependsOn   []Dependency
	Conditions  []Condition
}

// treeNode is the normalized tree: multi-segment keys like "a/b" are split
// into nested children so group metadata applies to a subtree regardless of
// whether the schema writes it nested or with flat slash keys.
type treeNode struct {
	markers  map[string]json.RawMessage // $repo / $file / $group
	children map[string]*treeNode
}

func newTreeNode() *treeNode {
	return &treeNode{markers: map[string]json.RawMessage{}, children: map[string]*treeNode{}}
}

func flattenDefinition(def *Definition) (Model, error) {
	model := Model{Entries: map[string]Entry{}}
	if def.Tree == nil {
		return model, errors.New("missing tree")
	}
	root := newTreeNode()
	if err := insertTreeMap(root, def.Tree, ""); err != nil {
		return model, err
	}
	if err := flattenTreeNode(root, "", inheritedGroup{}, &model); err != nil {
		return model, err
	}
	return model, nil
}

func insertTreeMap(node *treeNode, nodes map[string]json.RawMessage, prefix string) error {
	keys := make([]string, 0, len(nodes))
	for key := range nodes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if strings.HasPrefix(key, "$") {
			switch key {
			case "$repo", "$file", "$dir", "$group":
				if prefix == "" {
					return fmt.Errorf("reserved tree key %q cannot be used as a path segment", key)
				}
				node.markers[key] = nodes[key]
				continue
			default:
				return fmt.Errorf("reserved tree key %q cannot be used as a path segment", key)
			}
		}
		path, err := joinTreePath(prefix, key)
		if err != nil {
			return err
		}
		child := node
		for _, segment := range strings.Split(key, "/") {
			next, ok := child.children[segment]
			if !ok {
				next = newTreeNode()
				child.children[segment] = next
			}
			child = next
		}
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(nodes[key], &obj); err != nil {
			return fmt.Errorf("invalid tree node %s: %s", path, err)
		}
		if err := insertTreeMap(child, obj, path); err != nil {
			return err
		}
	}
	return nil
}

func flattenTreeNode(node *treeNode, path string, inherited inheritedGroup, model *Model) error {
	_, hasRepo := node.markers["$repo"]
	_, hasFile := node.markers["$file"]
	_, hasDir := node.markers["$dir"]
	groupRaw, hasGroup := node.markers["$group"]
	leafMarkers := 0
	for _, present := range []bool{hasRepo, hasFile, hasDir} {
		if present {
			leafMarkers++
		}
	}
	if leafMarkers > 1 {
		return fmt.Errorf("tree node %s can contain only one of $repo, $file, and $dir", path)
	}
	if leafMarkers == 1 {
		if len(node.children) > 0 || hasGroup {
			return fmt.Errorf("tree node %s cannot contain child nodes with $repo, $file, or $dir", path)
		}
		if err := validateSafePath(path); err != nil {
			return fmt.Errorf("invalid tree path %q: %s", path, err)
		}
		if hasRepo {
			var repo Repo
			if err := json.Unmarshal(node.markers["$repo"], &repo); err != nil {
				return fmt.Errorf("invalid $repo at %s: %s", path, err)
			}
			repo = applyInheritedRepo(repo, inherited)
			model.Entries[path] = Entry{
				Path:       path,
				Identity:   identityOr(repo.ID, path),
				Kind:       EntryRepo,
				Conditions: leafConditions(inherited, repo.OnlyWhen),
				Tags:       mergeTags(inherited.Tags, repo.Tags),
				Repo:       &repo,
			}
			return nil
		}
		if hasFile {
			var file File
			if err := json.Unmarshal(node.markers["$file"], &file); err != nil {
				return fmt.Errorf("invalid $file at %s: %s", path, err)
			}
			file = applyInheritedFile(file, inherited)
			model.Entries[path] = Entry{
				Path:       path,
				Identity:   identityOr(file.ID, path),
				Kind:       EntryFile,
				Conditions: leafConditions(inherited, file.OnlyWhen),
				Tags:       mergeTags(inherited.Tags, file.Tags),
				File:       &file,
			}
			return nil
		}
		var dir Dir
		if err := json.Unmarshal(node.markers["$dir"], &dir); err != nil {
			return fmt.Errorf("invalid $dir at %s: %s", path, err)
		}
		if dir.Description == "" {
			dir.Description = inherited.Description
		}
		if inherited.Archived {
			dir.Archived = true
		}
		model.Entries[path] = Entry{
			Path:       path,
			Identity:   identityOr(dir.ID, path),
			Kind:       EntryDir,
			Conditions: leafConditions(inherited, dir.OnlyWhen),
			Tags:       mergeTags(inherited.Tags, dir.Tags),
			Dir:        &dir,
		}
		return nil
	}
	if hasGroup {
		var group Group
		if err := json.Unmarshal(groupRaw, &group); err != nil {
			return fmt.Errorf("invalid $group at %s: %s", path, err)
		}
		inherited = mergeGroup(inherited, group)
		if inherited.Archived {
			group.Archived = true
		}
		model.Entries[path] = Entry{
			Path:       path,
			Identity:   identityOr(group.ID, path),
			Kind:       EntryGroup,
			Conditions: append([]Condition{}, inherited.Conditions...),
			Tags:       append([]string{}, inherited.Tags...),
			Group:      &group,
		}
	}
	childNames := make([]string, 0, len(node.children))
	for name := range node.children {
		childNames = append(childNames, name)
	}
	sort.Strings(childNames)
	for _, name := range childNames {
		childPath := name
		if path != "" {
			childPath = path + "/" + name
		}
		if err := flattenTreeNode(node.children[name], childPath, inherited, model); err != nil {
			return err
		}
	}
	return nil
}

// mergeTags unions inherited and declared tags, preserving first-seen order.
func mergeTags(inherited []string, own []string) []string {
	var merged []string
	seen := map[string]bool{}
	for _, tag := range append(append([]string{}, inherited...), own...) {
		if !seen[tag] {
			seen[tag] = true
			merged = append(merged, tag)
		}
	}
	return merged
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
		Tags:        mergeTags(inherited.Tags, group.Tags),
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
