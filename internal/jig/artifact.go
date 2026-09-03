package jig

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// A $file or $dir entry (an artifact) is materialized from a source list,
// or aliases another entry of its kind through link or copy. The two kinds
// differ in what a source resolves to (a blob, a tree) and in how content
// lands on disk; everything around that is shared here: following a schema
// move, telling a foreign symlink from a jig-owned one, the availability
// rules for sources, deactivation when every source is gated off, and link
// entries.

// materializer carries what every ensure call of one apply pass shares.
type materializer struct {
	out       io.Writer
	root      string
	model     *Model
	state     *State
	fetcher   *fileFetcher
	evidence  map[string]bool // repository evidence per-source conditions are judged against
	allowMove bool            // sync: written entries follow schema moves; clone refuses them
}

func newMaterializer(out io.Writer, root string, model *Model, state *State, fetcher *fileFetcher, activeRepos map[string]bool, installedRepos map[string]bool, allowMove bool) *materializer {
	return &materializer{
		out:       out,
		root:      root,
		model:     model,
		state:     state,
		fetcher:   fetcher,
		evidence:  evidenceSet(model, activeRepos, installedRepos),
		allowMove: allowMove,
	}
}

func (m *materializer) abs(rel string) string {
	return filepath.Join(m.root, rel)
}

// effectiveSources returns the sources an entry materializes from: its own
// for a src entry, its target's for a copy entry (the contents then match
// the target's by construction without requiring the target to be
// installed).
func effectiveSources(model *Model, entry Entry) (SrcList, error) {
	var own SrcList
	var copyRef *Ref
	var copyPath string
	switch entry.Kind {
	case EntryFile:
		own, copyRef, copyPath = entry.File.Src, entry.File.Copy, entry.File.copyPath
	case EntryDir:
		own, copyRef, copyPath = entry.Dir.Src, entry.Dir.Copy, entry.Dir.copyPath
	}
	if copyRef == nil {
		return own, nil
	}
	target, ok := model.entry(copyPath, entry.Kind)
	if !ok {
		return nil, fmt.Errorf("copy target is not defined: %s", describeRef(*copyRef))
	}
	if entry.Kind == EntryFile {
		return target.File.Src, nil
	}
	return target.Dir.Src, nil
}

// relocate follows a schema move: a record at another path is moved to the
// entry's path (sync) or refused (clone). A move is a plain rename, so
// local modifications travel with it like a dirty repository's do. It
// reports whether the record still stands: a recorded path that is gone
// from disk is dropped instead of moved.
func (m *materializer) relocate(entry Entry, recordedRel string) (bool, error) {
	if recordedRel == entry.Path {
		return true, nil
	}
	if !pathEntryExists(m.abs(recordedRel)) {
		m.state.drop(entry.Kind, entry.Identity)
		return false, nil
	}
	if !m.allowMove {
		return false, fmt.Errorf("already written at %s; run jig sync to move it", recordedRel)
	}
	message, err := moveInstalledPath(m.root, entry.Path, recordedRel, entry.Path, "moved-"+string(entry.Kind))
	if err != nil {
		return false, err
	}
	fmt.Fprintln(m.out, message)
	return true, nil
}

// clearOwnedLink handles a symlink found where a real file or directory is
// expected (pathExists would follow it, and writing through it would land
// in the target). Only a jig-owned link, a link entry turned src or copy,
// is removed to make room; any other symlink is a foreign path. It reports
// whether the record went with the link.
func (m *materializer) clearOwnedLink(entry Entry, isLinkRecord bool) (bool, error) {
	abs := m.abs(entry.Path)
	if !isSymlink(abs) {
		return false, nil
	}
	if !isLinkRecord {
		return false, fmt.Errorf("existing path is a symlink: %s", entry.Path)
	}
	if err := os.Remove(abs); err != nil {
		return false, err
	}
	m.state.drop(entry.Kind, entry.Identity)
	return true, nil
}

// allGatedOff reports whether every source is switched off by its
// condition, before any of them is fetched.
func (m *materializer) allGatedOff(srcs SrcList) bool {
	for _, source := range srcs {
		if source.OnlyWhen == nil || conditionMetIn(m.model, m.evidence, *source.OnlyWhen) {
			return false
		}
	}
	return true
}

// definitionError marks a source failure that is a schema bug (a malformed
// spec, a path outside the repository) rather than an availability
// problem: it fails the entry instead of being reported as unavailable.
type definitionError struct{ err error }

func (e definitionError) Error() string { return e.err.Error() }

// resolvedSources is what resolving a source list yields: the keys of the
// sources that resolved, in list order, and the required ones that did not.
type resolvedSources struct {
	keys        []string
	unavailable []string
}

// none reports that nothing resolved and nothing is missing: every source
// was gated off or optional and absent, which is the same as no source.
func (r resolvedSources) none() bool { return len(r.keys) == 0 && len(r.unavailable) == 0 }

// srcKey is the state's record of which sources produced the content.
func (r resolvedSources) srcKey() string { return strings.Join(r.keys, " ") }

// note is the report suffix naming the unavailable sources, if any.
func (r resolvedSources) note() string {
	if len(r.unavailable) == 0 {
		return ""
	}
	return " (" + strings.Join(r.unavailable, "; ") + ")"
}

func (r resolvedSources) unavailableError() error {
	return errors.New(strings.Join(r.unavailable, "; "))
}

// resolveSources walks a source list under the shared availability rules,
// before anything is written: a source gated off by its condition is
// skipped; a malformed spec fails the entry; a source that fails to
// resolve (unreachable repository, content missing upstream, absent local
// path) is skipped when optional and reported as unavailable otherwise, so
// one broken source does not block the entry. resolve does the
// kind-specific work for one source and returns the key that identifies it
// in the state's src field.
func (m *materializer) resolveSources(srcs SrcList, resolve func(source SrcEntry) (string, error)) (resolvedSources, error) {
	var result resolvedSources
	for _, source := range srcs {
		if source.OnlyWhen != nil && !conditionMetIn(m.model, m.evidence, *source.OnlyWhen) {
			continue
		}
		key, err := resolve(source)
		if err != nil {
			var definition definitionError
			if errors.As(err, &definition) {
				return result, fmt.Errorf("source %s: %s", source.describe(), shortError(err))
			}
			if source.Optional {
				continue
			}
			result.unavailable = append(result.unavailable, fmt.Sprintf("source %s unavailable: %s", source.describe(), shortError(err)))
			continue
		}
		result.keys = append(result.keys, key)
	}
	return result, nil
}

// deleteArtifact takes the entry's written content off disk under the
// shared safety rules, reporting how many modified files were kept.
func (m *materializer) deleteArtifact(entry Entry, policy modifiedPolicy) (int, error) {
	switch entry.Kind {
	case EntryFile:
		kept, err := deleteTrackedFile(m.root, entry.Path, m.state.Files[entry.Identity], policy)
		if kept {
			return 1, err
		}
		return 0, err
	case EntryDir:
		return deleteTrackedDir(m.root, entry.Path, m.state.Dirs[entry.Identity], policy)
	}
	return 0, nil
}

// ensureWithoutSources converges an entry whose sources are all gated off
// or optional and absent: nothing is written, a previously written
// untouched entry is removed, and locally modified content is kept but
// abandoned as untracked, like the modified files of a single deactivated
// dir source.
func (m *materializer) ensureWithoutSources(entry Entry, hasState bool) error {
	kind := string(entry.Kind)
	if !hasState {
		fmt.Fprintf(m.out, "inactive-%s: %s (no active sources)\n", kind, entry.Path)
		return nil
	}
	existed := pathEntryExists(m.abs(entry.Path))
	kept, err := m.deleteArtifact(entry, abandonModified)
	if err != nil {
		return err
	}
	m.state.drop(entry.Kind, entry.Identity)
	switch {
	case kept > 0:
		fmt.Fprintf(m.out, "inactive-%s: %s (no active sources; %s left untracked)\n", kind, entry.Path, countNoun(kept, "modified file"))
	case existed:
		fmt.Fprintf(m.out, "removed-%s: %s (no active sources)\n", kind, entry.Path)
	default:
		fmt.Fprintf(m.out, "inactive-%s: %s (no active sources)\n", kind, entry.Path)
	}
	return nil
}

// ensureLink makes the entry a relative symlink to its link target.
func (m *materializer) ensureLink(entry Entry) error {
	var linkRef *Ref
	var linkPath string
	switch entry.Kind {
	case EntryFile:
		linkRef, linkPath = entry.File.Link, entry.File.linkPath
	case EntryDir:
		linkRef, linkPath = entry.Dir.Link, entry.Dir.linkPath
	}
	targetEntry, ok := m.model.entry(linkPath, entry.Kind)
	if !ok {
		return fmt.Errorf("link target is not defined: %s", describeRef(*linkRef))
	}
	if !pathExists(m.abs(targetEntry.Path)) {
		return fmt.Errorf("link target is missing: %s", targetEntry.Path)
	}
	expectedAbs := m.abs(entry.Path)
	expectedTarget, err := relativeSymlinkTarget(entry.Path, targetEntry.Path)
	if err != nil {
		return err
	}
	kind := string(entry.Kind)

	recordPath, recordLink, hasState := m.state.artifactRecord(entry.Kind, entry.Identity)
	if hasState {
		if hasState, err = m.relocate(entry, recordPath); err != nil {
			return err
		}
	}

	if pathEntryExists(expectedAbs) {
		info, err := os.Lstat(expectedAbs)
		if err != nil {
			return err
		}
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			currentTarget, err := os.Readlink(expectedAbs)
			if err != nil {
				return err
			}
			if currentTarget == expectedTarget {
				m.state.setLinkRecord(entry.Kind, entry.Identity, entry.Path, linkPath)
				fmt.Fprintf(m.out, "present-%s: %s\n", kind, entry.Path)
				return nil
			}
			if !hasState || recordLink == "" {
				return errors.New("existing symlink has different target")
			}
			if err := os.Remove(expectedAbs); err != nil {
				return err
			}
		case hasState && recordLink == "":
			// Converting a src or copy entry to a link: an untouched
			// materialization is replaced by the symlink; modified content
			// blocks the conversion.
			if _, err := m.deleteArtifact(entry, refuseModified); err != nil {
				return fmt.Errorf("%s; refusing to replace with a symlink: %s", err, entry.Path)
			}
			removeEmptyDirTree(expectedAbs)
			if pathEntryExists(expectedAbs) {
				return fmt.Errorf("directory was not fully cleared (untracked files remain, or a removal failed); refusing to replace with a symlink: %s", entry.Path)
			}
		default:
			return fmt.Errorf("expected symlink path exists and is not a symlink: %s", entry.Path)
		}
	}

	if err := os.MkdirAll(filepath.Dir(expectedAbs), 0o755); err != nil {
		return err
	}
	if err := makeSymlink(expectedTarget, expectedAbs); err != nil {
		return err
	}
	m.state.setLinkRecord(entry.Kind, entry.Identity, entry.Path, linkPath)
	fmt.Fprintf(m.out, "linked-%s: %s\n", kind, entry.Path)
	return nil
}

// removeEmptyDirTree removes path and its subdirectories bottom-up as far as
// they are empty; anything non-empty is left in place for the caller to
// inspect.
func removeEmptyDirTree(path string) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() {
			removeEmptyDirTree(filepath.Join(path, entry.Name()))
		}
	}
	_ = os.Remove(path)
}

// countNoun formats a count with its noun, pluralized with a plain s.
func countNoun(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
