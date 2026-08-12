package jig

import "strings"

// resolveRef returns the entries a reference selects, in path order: the one
// entry of an id or exact-path ref, every entry strictly below a subtree
// path ("p/*" or bare "*"), or every repository carrying all the tags.
// Kind filtering is the caller's: dependencies and conditions keep
// repositories, links require their own kind.
func (model *Model) resolveRef(ref Ref) []Entry {
	switch {
	case ref.ID != "":
		for _, path := range sortedEntryPaths(model) {
			if entry := model.Entries[path]; entry.Identity == ref.ID {
				return []Entry{entry}
			}
		}
		return nil
	case ref.Path != "":
		base, subtree := subtreePath(ref.Path)
		if !subtree {
			if entry, ok := model.Entries[ref.Path]; ok {
				return []Entry{entry}
			}
			return nil
		}
		var entries []Entry
		for _, path := range sortedEntryPaths(model) {
			if base == "" || strings.HasPrefix(path, base+"/") {
				entries = append(entries, model.Entries[path])
			}
		}
		return entries
	case len(ref.Tags) > 0:
		var entries []Entry
		for _, path := range sortedEntryPaths(model) {
			entry := model.Entries[path]
			if entry.Kind == EntryRepo && entry.hasAllTags(ref.Tags) {
				entries = append(entries, entry)
			}
		}
		return entries
	default:
		return nil
	}
}

// resolveRepoRef is resolveRef narrowed to repositories, the target domain
// of dependencies and conditions.
func (model *Model) resolveRepoRef(ref Ref) []Entry {
	var repos []Entry
	for _, entry := range model.resolveRef(ref) {
		if entry.Kind == EntryRepo {
			repos = append(repos, entry)
		}
	}
	return repos
}

// resolveLinkPaths caches each link entry's target path after flattening.
// Refs that do not resolve to exactly one entry of the same kind leave the
// cache empty; validation reports why, and apply-time code fails closed.
func resolveLinkPaths(model *Model) {
	for _, entry := range model.Entries {
		switch {
		case entry.Kind == EntryFile && entry.File.Link != nil:
			entry.File.linkPath = linkTargetPath(model, *entry.File.Link, EntryFile)
		case entry.Kind == EntryDir && entry.Dir.Link != nil:
			entry.Dir.linkPath = linkTargetPath(model, *entry.Dir.Link, EntryDir)
		}
	}
}

func linkTargetPath(model *Model, ref Ref, kind EntryKind) string {
	if ref.selectorCount() != 1 || len(ref.Tags) > 0 {
		return ""
	}
	if _, subtree := subtreePath(ref.Path); subtree {
		return ""
	}
	entries := model.resolveRef(ref)
	if len(entries) == 1 && entries[0].Kind == kind {
		return entries[0].Path
	}
	return ""
}
