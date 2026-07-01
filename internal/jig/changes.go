package jig

import (
	"fmt"
	"io"
	"reflect"
)

func printDefinitionChanges(out io.Writer, oldModel *Model, newModel *Model) {
	printEntryChanges(out, "repo", repoIdentityToPath(oldModel), repoIdentityToPath(newModel), repoChanged(oldModel, newModel))
	printEntryChanges(out, "file", fileIdentityToPath(oldModel), fileIdentityToPath(newModel), fileChanged(oldModel, newModel))
}

func printEntryChanges(out io.Writer, label string, oldByID map[string]string, newByID map[string]string, changedByID map[string]bool) {
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
		if changedByID[identity] {
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

func repoChanged(oldModel *Model, newModel *Model) map[string]bool {
	result := map[string]bool{}
	oldByID := repoIdentityToPath(oldModel)
	newByID := repoIdentityToPath(newModel)
	for identity, newPath := range newByID {
		oldPath, ok := oldByID[identity]
		if !ok {
			continue
		}
		oldEntry := oldModel.Repos[oldPath]
		newEntry := newModel.Repos[newPath]
		oldRepo := oldEntry.Repo
		newRepo := newEntry.Repo
		if oldRepo.Git != newRepo.Git || oldRepo.Web != newRepo.Web || oldRepo.Description != newRepo.Description || !reflect.DeepEqual(oldRepo.DependsOn, newRepo.DependsOn) || !reflect.DeepEqual(oldEntry.Conditions, newEntry.Conditions) {
			result[identity] = true
		}
	}
	return result
}

func fileChanged(oldModel *Model, newModel *Model) map[string]bool {
	result := map[string]bool{}
	oldByID := fileIdentityToPath(oldModel)
	newByID := fileIdentityToPath(newModel)
	for identity, newPath := range newByID {
		oldPath, ok := oldByID[identity]
		if !ok {
			continue
		}
		oldEntry := oldModel.Files[oldPath]
		newEntry := newModel.Files[newPath]
		oldFile := oldEntry.File
		newFile := newEntry.File
		if oldFile.Src != newFile.Src || oldFile.Link != newFile.Link || oldFile.Description != newFile.Description || oldFile.Executable != newFile.Executable || !reflect.DeepEqual(oldEntry.Conditions, newEntry.Conditions) {
			result[identity] = true
		}
	}
	return result
}
