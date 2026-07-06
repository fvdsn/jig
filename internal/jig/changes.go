package jig

import (
	"fmt"
	"io"
	"reflect"
)

func printDefinitionChanges(out io.Writer, oldModel *Model, newModel *Model) {
	printEntryChanges(out, "repo", oldModel, newModel, EntryRepo)
	printEntryChanges(out, "file", oldModel, newModel, EntryFile)
	printEntryChanges(out, "dir", oldModel, newModel, EntryDir)
	printEntryChanges(out, "group", oldModel, newModel, EntryGroup)
}

func printEntryChanges(out io.Writer, label string, oldModel *Model, newModel *Model, kind EntryKind) {
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

func entryChanged(oldEntry, newEntry Entry) bool {
	return !reflect.DeepEqual(oldEntry.Repo, newEntry.Repo) ||
		!reflect.DeepEqual(oldEntry.File, newEntry.File) ||
		!reflect.DeepEqual(oldEntry.Dir, newEntry.Dir) ||
		!reflect.DeepEqual(oldEntry.Group, newEntry.Group) ||
		!reflect.DeepEqual(oldEntry.Conditions, newEntry.Conditions) ||
		!reflect.DeepEqual(oldEntry.Tags, newEntry.Tags)
}
