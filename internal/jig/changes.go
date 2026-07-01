package jig

import (
	"fmt"
	"io"
	"reflect"
)

func printDefinitionChanges(out io.Writer, oldModel *Model, newModel *Model) {
	printEntryChanges(out, "repo", oldModel, newModel, EntryRepo, repoEntryChanged)
	printEntryChanges(out, "file", oldModel, newModel, EntryFile, fileEntryChanged)
	printEntryChanges(out, "group", oldModel, newModel, EntryGroup, groupEntryChanged)
}

func printEntryChanges(out io.Writer, label string, oldModel *Model, newModel *Model, kind EntryKind, entryChanged func(oldEntry, newEntry Entry) bool) {
	oldByID := identityToPath(oldModel, kind)
	newByID := identityToPath(newModel, kind)
	var added []string
	var removed []string
	var moved []string
	var changed []string
	for identity, newPath := range newByID {
		oldPath, ok := oldByID[identity]
		if !ok {
			added = append(added, newPath)
			continue
		}
		if oldPath != newPath {
			moved = append(moved, fmt.Sprintf("%s: %s -> %s", identity, oldPath, newPath))
		}
		oldEntry, _ := oldModel.entry(oldPath, kind)
		newEntry, _ := newModel.entry(newPath, kind)
		if entryChanged(oldEntry, newEntry) {
			changed = append(changed, newPath)
		}
	}
	for identity, oldPath := range oldByID {
		if _, ok := newByID[identity]; !ok {
			removed = append(removed, oldPath)
		}
	}
	printGroup(out, label+"-added", added)
	printGroup(out, label+"-removed", removed)
	printGroup(out, label+"-moved", moved)
	printGroup(out, label+"-changed", changed)
}

func repoEntryChanged(oldEntry, newEntry Entry) bool {
	oldRepo, newRepo := oldEntry.Repo, newEntry.Repo
	return oldRepo.Git != newRepo.Git ||
		oldRepo.Web != newRepo.Web ||
		oldRepo.Description != newRepo.Description ||
		!reflect.DeepEqual(oldRepo.DependsOn, newRepo.DependsOn) ||
		!reflect.DeepEqual(oldEntry.Conditions, newEntry.Conditions)
}

func fileEntryChanged(oldEntry, newEntry Entry) bool {
	oldFile, newFile := oldEntry.File, newEntry.File
	return oldFile.Src != newFile.Src ||
		oldFile.Link != newFile.Link ||
		oldFile.Description != newFile.Description ||
		oldFile.Executable != newFile.Executable ||
		!reflect.DeepEqual(oldEntry.Conditions, newEntry.Conditions)
}

func groupEntryChanged(oldEntry, newEntry Entry) bool {
	return !reflect.DeepEqual(oldEntry.Group, newEntry.Group) ||
		!reflect.DeepEqual(oldEntry.Conditions, newEntry.Conditions)
}
