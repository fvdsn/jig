package jig

import (
	"archive/tar"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// ensureDir materializes a $dir entry: a whole subtree of a source
// repository, or the merge of several. State keeps the source tree ids plus
// a manifest of every file written, so updates overwrite only untouched
// files, deletions remove only untouched files, and user files inside the
// directory are never touched.
func (m *materializer) ensureDir(dirPath string) error {
	entry, _ := m.model.entry(dirPath, EntryDir)
	dir := entry.Dir
	if dir.Link != nil {
		return m.ensureLink(entry)
	}
	srcs, err := effectiveSources(m.model, entry)
	if err != nil {
		return err
	}
	expectedAbs := m.abs(entry.Path)

	record, hasState := m.state.Dirs[entry.Identity]
	if hasState {
		if hasState, err = m.relocate(entry, record.Path); err != nil {
			return err
		}
		if hasState {
			record.Path = entry.Path
			m.state.Dirs[entry.Identity] = record
		}
	}
	if m.allGatedOff(srcs) {
		return m.ensureWithoutSources(entry, hasState)
	}
	if cleared, err := m.clearOwnedLink(entry, hasState && record.Link != ""); err != nil {
		return err
	} else if cleared {
		record, hasState = StateDir{}, false
	}

	skillsLayout := skillsLayoutOf(m.model, entry)
	items := dir.items
	var sources []dirSource
	var treeOIDs []string
	var skippedPlugins []string
	resolved, err := m.resolveSources(srcs, func(source SrcEntry) (string, error) {
		if source.plugin != "" {
			list, key, id, skipped, err := m.resolvePluginSource(source)
			if err != nil {
				return "", err
			}
			sources = append(sources, list...)
			treeOIDs = append(treeOIDs, id)
			skippedPlugins = append(skippedPlugins, skipped...)
			return key, nil
		}
		if source.File != "" {
			// Only items dirs accept file sources; validation enforces it.
			localAbs, err := expandLocalSource(source.File)
			if err != nil {
				return "", definitionError{err}
			}
			content, err := os.ReadFile(localAbs)
			if err != nil {
				return "", err
			}
			digest := sha256Hex(content)
			sources = append(sources, dirSource{name: filepath.Base(localAbs), file: content, tree: digest})
			treeOIDs = append(treeOIDs, digest)
			return "file:" + source.File, nil
		}
		if source.Dir != "" {
			localAbs, err := expandLocalSource(source.Dir)
			if err != nil {
				return "", definitionError{err}
			}
			digest, err := localTreeDigest(localAbs)
			if err != nil {
				return "", err
			}
			prefix := ""
			if dir.store || (skillsLayout && pathExists(filepath.Join(localAbs, skillFile))) {
				prefix = filepath.Base(localAbs) + "/"
			}
			sources = append(sources, dirSource{local: localAbs, tree: digest, prefix: prefix})
			treeOIDs = append(treeOIDs, digest)
			return "dir:" + source.Dir, nil
		}
		parse := parseDirSrc
		if items {
			parse = parseItemSrc
		}
		parsed, err := parse(source.Src)
		if err != nil {
			return "", definitionError{err}
		}
		mirror, err := m.fetcher.mirror(parsed.GitURL)
		if err != nil {
			return "", err
		}
		srcPath, err := resolveSrcPath(mirror, parsed)
		if err != nil {
			return "", definitionError{err}
		}
		treeRef := "HEAD^{tree}"
		if srcPath != "" {
			treeRef = "HEAD:" + srcPath
		}
		treeOut, err := git(mirror, "rev-parse", treeRef)
		if err != nil {
			return "", fmt.Errorf("subtree not found: %s", shortError(err))
		}
		treeOID := strings.TrimSpace(treeOut)
		objType, err := git(mirror, "cat-file", "-t", treeOID)
		if err != nil {
			return "", fmt.Errorf("%s is not a directory in the source repository", srcPath)
		}
		switch strings.TrimSpace(objType) {
		case "tree":
		case "blob":
			if !items {
				return "", fmt.Errorf("%s is not a directory in the source repository", srcPath)
			}
			content, err := git(mirror, "cat-file", "-p", treeOID)
			if err != nil {
				return "", err
			}
			sources = append(sources, dirSource{name: path.Base(srcPath), file: []byte(content), tree: treeOID})
			treeOIDs = append(treeOIDs, treeOID)
			return source.Src, nil
		default:
			return "", fmt.Errorf("%s is not a directory in the source repository", srcPath)
		}
		prefix := ""
		if dir.store {
			prefix = sourceBaseName(parsed, srcPath) + "/"
		} else if skillsLayout {
			if _, err := git(mirror, "cat-file", "-e", treeOID+":"+skillFile); err == nil {
				prefix = sourceBaseName(parsed, srcPath) + "/"
			}
		}
		sources = append(sources, dirSource{mirror: mirror, tree: treeOID, prefix: prefix})
		treeOIDs = append(treeOIDs, treeOID)
		return source.Src, nil
	})
	if err != nil {
		return err
	}
	if resolved.none() {
		return m.ensureWithoutSources(entry, hasState)
	}
	srcKey := resolved.srcKey()
	combinedTree := strings.Join(treeOIDs, "+")
	note := resolved.note()

	// Sources that resolved but contribute nothing (plugins without this
	// directory) are the same as no source at all. With a source
	// unavailable there is nothing to materialize: an already-written
	// directory is left as is, a missing one is an error.
	if len(sources) == 0 {
		if len(resolved.unavailable) == 0 {
			return m.ensureWithoutSources(entry, hasState)
		}
		if hasState && pathExists(expectedAbs) {
			fmt.Fprintf(m.out, "present-dir: %s%s\n", dirPath, note)
			return nil
		}
		return resolved.unavailableError()
	}

	if hasState && record.Src == srcKey && record.Tree == combinedTree && manifestClean(expectedAbs, record.Files) {
		fmt.Fprintf(m.out, "present-dir: %s%s\n", dirPath, note)
		return nil
	}

	oldManifest := map[string]string{}
	if hasState {
		oldManifest = record.Files
	}
	merger := &dirMerger{transform: dir.transform, entryPath: entry.Path, dirAbs: expectedAbs, oldManifest: oldManifest, manifest: map[string]string{}}
	// Later sources override earlier ones, so a source list reads as base
	// layers first and overrides after. Materializing in reverse order makes
	// the first-claim rule award a conflict to the last listed source.
	for i := len(sources) - 1; i >= 0; i-- {
		merger.prefix, merger.rewrite, merger.routes = sources[i].prefix, sources[i].rewrite, sources[i].routes
		var err error
		switch {
		case sources[i].name != "":
			err = merger.add(sources[i].name, sources[i].file, 0o644)
		case sources[i].local != "":
			err = materializeLocalTree(sources[i].local, merger)
		default:
			err = materializeTree(sources[i].mirror, sources[i].tree, merger, sources[i].pathspecs...)
		}
		if err != nil {
			return err
		}
	}
	newManifest, counts := merger.manifest, merger.counts

	// Files that disappeared upstream: delete only untouched ones. While
	// any source is unavailable its files cannot be told apart from truly
	// deleted ones, so nothing is deleted and vanished entries stay
	// tracked until every source resolves again.
	for rel, oldHash := range oldManifest {
		if _, stillThere := newManifest[rel]; stillThere {
			continue
		}
		if len(resolved.unavailable) > 0 {
			newManifest[rel] = oldHash
			continue
		}
		target := filepath.Join(expectedAbs, filepath.FromSlash(rel))
		if !pathEntryExists(target) {
			continue
		}
		if isSymlink(target) {
			counts.abandoned++
			continue
		}
		localHash, err := fileSHA256(target)
		if err == nil && localHash == oldHash {
			if err := os.Remove(target); err != nil {
				return err
			}
			pruneEmptyParents(m.root, filepath.Dir(filepath.Join(entry.Path, filepath.FromSlash(rel))))
			counts.deleted++
		} else {
			counts.abandoned++
		}
	}

	m.state.Dirs[entry.Identity] = StateDir{Path: entry.Path, Src: srcKey, Tree: combinedTree, Files: newManifest}
	fmt.Fprintln(m.out, dirMessage(dirPath, hasState, counts)+note)
	for _, name := range skippedPlugins {
		fmt.Fprintf(m.out, "  skipped plugin %s: its marketplace source is not a path inside the repository\n", name)
	}
	return nil
}

// dirSource is one resolved source of a dir: a git tree in a mirror, a
// local directory (its content digest standing in for a tree id), or a
// single file (items dirs only) carrying its content.
type dirSource struct {
	mirror    string
	tree      string
	local     string         // local directory source; mirror is empty
	prefix    string         // directory the source's files land under; "" for the root
	name      string         // single-file source: the file's basename
	file      []byte         // single-file source: its content
	rewrite   *pluginRewrite // plugin subtree: how to rewrite its links into the store
	routes    []pluginRoute  // plugin source: which files to take and how, by prefix
	pathspecs []string       // plugin source: the subtrees to archive
}

// pluginRoute maps a path prefix inside a plugin source's tree
// ("alpha/skills/") to the plugin whose files it holds, so one archive of
// the whole source can be routed file by file.
type pluginRoute struct {
	prefix  string
	rewrite *pluginRewrite
}

// pluginResolution is what resolving a plugin source for one primitive
// yields, memoized for the run so the copies of a primitive (the Claude
// skills next to the canonical ones) do not ask git again.
type pluginResolution struct {
	source  dirSource
	key     string
	id      string
	skipped []string
}

// resolvePluginSource expands a plugin or marketplace source into one
// dirSource covering the subtrees (skills/, agents/, or commands/) of
// every plugin it holds, routed by prefix to each plugin's link-rewriting
// context. The whole source costs three git calls: the manifest, one
// listing of every file, and later one archive with the subtrees as
// pathspecs. It returns the state key of the source, the tree id covering
// all of it, and the names of marketplace entries skipped because they
// are not paths or do not exist.
func (m *materializer) resolvePluginSource(source SrcEntry) ([]dirSource, string, string, []string, error) {
	cacheKey := source.describe() + "\x00" + source.plugin
	if cached, ok := m.plugins[cacheKey]; ok {
		if len(cached.source.routes) == 0 {
			return nil, cached.key, cached.id, cached.skipped, nil
		}
		return []dirSource{cached.source}, cached.key, cached.id, cached.skipped, nil
	}
	var root pluginTree
	var base, key, id string
	if source.Dir != "" {
		localAbs, err := expandLocalSource(source.Dir)
		if err != nil {
			return nil, "", "", nil, definitionError{err}
		}
		digest, err := localTreeDigest(localAbs)
		if err != nil {
			return nil, "", "", nil, err
		}
		root, base, key, id = pluginTree{local: localAbs}, filepath.Base(localAbs), "dir:"+source.Dir, digest
	} else {
		parsed, err := parseDirSrc(source.Src)
		if err != nil {
			return nil, "", "", nil, definitionError{err}
		}
		mirror, err := m.fetcher.mirror(parsed.GitURL)
		if err != nil {
			return nil, "", "", nil, err
		}
		srcPath, err := resolveSrcPath(mirror, parsed)
		if err != nil {
			return nil, "", "", nil, definitionError{err}
		}
		ref := "HEAD^{tree}"
		if srcPath != "" {
			ref = "HEAD:" + srcPath
		}
		out, err := git(mirror, "rev-parse", ref)
		if err != nil {
			return nil, "", "", nil, fmt.Errorf("subtree not found: %s", shortError(err))
		}
		root, base, key, id = pluginTree{mirror: mirror, tree: strings.TrimSpace(out)}, sourceBaseName(parsed, srcPath), source.Src, strings.TrimSpace(out)
	}
	plugins, skipped, err := root.manifest()
	if err != nil {
		return nil, "", "", nil, err
	}
	all, err := root.files()
	if err != nil {
		return nil, "", "", nil, err
	}
	var routes []pluginRoute
	var pathspecs []string
	for _, rel := range plugins {
		pluginPrefix := ""
		if rel != "" {
			pluginPrefix = rel + "/"
		}
		files := map[string]bool{}
		for file := range all {
			if inner, ok := strings.CutPrefix(file, pluginPrefix); ok {
				files[inner] = true
			}
		}
		if len(files) == 0 {
			skipped = append(skipped, rel)
			continue
		}
		subdirPrefix := source.plugin + "/"
		hasSubdir := false
		for file := range files {
			if strings.HasPrefix(file, subdirPrefix) {
				hasSubdir = true
				break
			}
		}
		if !hasSubdir {
			continue
		}
		routes = append(routes, pluginRoute{
			prefix:  pluginPrefix + subdirPrefix,
			rewrite: &pluginRewrite{storePath: path.Join(agentsPluginsPath, base, rel), files: files, subdir: source.plugin},
		})
		pathspecs = append(pathspecs, pluginPrefix+source.plugin)
	}
	resolution := pluginResolution{key: key, id: id, skipped: skipped}
	if len(routes) > 0 {
		resolution.source = dirSource{mirror: root.mirror, tree: root.tree, local: root.local, routes: routes, pathspecs: pathspecs}
	}
	if m.plugins == nil {
		m.plugins = map[string]pluginResolution{}
	}
	m.plugins[cacheKey] = resolution
	if len(routes) == 0 {
		return nil, key, id, skipped, nil
	}
	return []dirSource{resolution.source}, key, id, skipped, nil
}

// skillFile marks a directory as one skill. A skills-layout source whose
// root holds it is a single skill rather than a pack of them, and lands
// under its own name so the harness still sees <name>/SKILL.md.
const skillFile = "SKILL.md"

// skillsLayoutOf reports whether a dir entry merges skill sources, through
// its own flag or, for a copy alias, its target's.
func skillsLayoutOf(model *Model, entry Entry) bool {
	if entry.Dir.skills {
		return true
	}
	if entry.Dir.Copy != nil {
		if target, ok := model.entry(entry.Dir.copyPath, EntryDir); ok {
			return target.Dir.skills
		}
	}
	return false
}

// sourceBaseName names a git source by the last segment of its subtree
// path, or of the repository URL when the whole tree is the source.
func sourceBaseName(parsed fileSrc, srcPath string) string {
	if srcPath != "" {
		return filepath.Base(filepath.FromSlash(srcPath))
	}
	url := strings.TrimSuffix(strings.TrimRight(parsed.GitURL, "/"), ".git")
	if i := strings.LastIndexAny(url, "/:"); i >= 0 {
		url = url[i+1:]
	}
	return url
}

type dirCounts struct {
	added     int
	updated   int
	unchanged int
	kept      int // locally modified files that were not overwritten
	deleted   int // removed because they vanished upstream and were untouched
	abandoned int // vanished upstream but locally modified; left as untracked
	shadowed  int // provided by more than one source; the last source won
}

func dirMessage(dirPath string, hadState bool, counts dirCounts) string {
	if counts.added+counts.updated+counts.kept+counts.deleted+counts.abandoned+counts.shadowed == 0 {
		return "present-dir: " + dirPath
	}
	verb := "wrote-dir"
	if hadState {
		verb = "updated-dir"
	}
	var parts []string
	add := func(n int, label string) {
		if n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, label))
		}
	}
	add(counts.added, "added")
	add(counts.updated, "updated")
	add(counts.deleted, "deleted")
	add(counts.kept, "modified kept")
	add(counts.abandoned, "left untracked")
	add(counts.shadowed, "shadowed")
	return fmt.Sprintf("%s: %s (%s)", verb, dirPath, strings.Join(parts, ", "))
}

// manifestClean reports whether every manifest file exists locally with the
// recorded content.
func manifestClean(dirAbs string, manifest map[string]string) bool {
	for rel, recorded := range manifest {
		path := filepath.Join(dirAbs, filepath.FromSlash(rel))
		if isSymlink(path) {
			return false
		}
		hash, err := fileSHA256(path)
		if err != nil || hash != recorded {
			return false
		}
	}
	return true
}

// dirMerger applies source files to a merged directory: each file lands
// under the current source's prefix, through the dir's transform when it
// has one, under the manifest rules of mergeFileIntoDir.
type dirMerger struct {
	prefix      string
	transform   string
	rewrite     *pluginRewrite // set while a plugin subtree is being merged
	routes      []pluginRoute  // plugin source: files are routed by prefix, others skipped
	entryPath   string         // workspace path of the dir, for link rewriting
	dirAbs      string
	oldManifest map[string]string
	manifest    map[string]string
	counts      dirCounts
}

func (g *dirMerger) add(rel string, content []byte, mode os.FileMode) error {
	if g.routes != nil {
		routed := false
		for _, route := range g.routes {
			if inner, ok := strings.CutPrefix(rel, route.prefix); ok {
				rel, g.rewrite, routed = inner, route.rewrite, true
				break
			}
		}
		if !routed {
			return nil
		}
	}
	if g.rewrite != nil {
		content = g.rewrite.rewriteLinks(rel, content, path.Dir(g.entryPath+"/"+g.prefix+rel))
	}
	if transform := dirTransforms[g.transform]; transform != nil {
		rel, content = transform(rel, content)
	}
	return mergeFileIntoDir(g.prefix+rel, content, mode, g.dirAbs, g.oldManifest, g.manifest, &g.counts)
}

// materializeTree streams `git archive` of the tree from the mirror into
// the merged directory. Files already claimed by a source materialized
// before this one are shadowed (sources apply in reverse list order, so
// the last listed source claims a conflicted path first); files matching
// the old manifest (untouched) are overwritten; locally modified files are
// kept and counted.
func materializeTree(mirror string, treeOID string, merger *dirMerger, pathspecs ...string) error {
	cmd := exec.Command("git", append([]string{"archive", treeOID}, pathspecs...)...)
	cmd.Dir = mirror
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	if err := mergeArchive(stdout, merger); err != nil {
		// Stopping mid-stream leaves git blocked on a pipe nobody reads;
		// kill it so Wait returns instead of hanging.
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return err
	}
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("git archive: %s", err)
	}
	return nil
}

// mergeArchive merges every regular file of a tar stream.
func mergeArchive(stream io.Reader, merger *dirMerger) error {
	reader := tar.NewReader(stream)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		rel := filepath.ToSlash(filepath.Clean(header.Name))
		content, err := io.ReadAll(reader)
		if err != nil {
			return err
		}
		if err := merger.add(rel, content, header.FileInfo().Mode().Perm()); err != nil {
			return err
		}
	}
}

// mergeFileIntoDir applies one source file to the merged directory under the
// manifest rules: paths already claimed by a source materialized earlier are
// shadowed, untouched files are overwritten, locally modified files kept.
func mergeFileIntoDir(rel string, content []byte, mode os.FileMode, dirAbs string, oldManifest map[string]string, manifest map[string]string, counts *dirCounts) error {
	if err := validateSafePath(rel); err != nil {
		return fmt.Errorf("unsafe path in source tree: %q", rel)
	}
	if _, claimed := manifest[rel]; claimed {
		counts.shadowed++
		return nil
	}
	newHash := sha256Hex(content)
	manifest[rel] = newHash

	target := filepath.Join(dirAbs, filepath.FromSlash(rel))
	if pathEntryExists(target) {
		// A symlink here cannot be a file this $dir wrote (manifests track
		// regular files only, and hashing through the link may fail on
		// loops); keep it like a locally modified file.
		if isSymlink(target) {
			counts.kept++
			return nil
		}
		localHash, err := fileSHA256(target)
		if err != nil {
			return err
		}
		if localHash == newHash {
			counts.unchanged++
			return nil
		}
		oldHash, tracked := oldManifest[rel]
		if !tracked || localHash != oldHash {
			counts.kept++
			return nil
		}
		if err := os.WriteFile(target, content, mode); err != nil {
			return err
		}
		_ = os.Chmod(target, mode)
		counts.updated++
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(target, content, mode); err != nil {
		return err
	}
	counts.added++
	return nil
}

// materializeLocalTree merges a local directory's regular files with the
// same rules as a git tree source.
func materializeLocalTree(localAbs string, merger *dirMerger) error {
	return filepath.WalkDir(localAbs, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(localAbs, path)
		if err != nil {
			return err
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return merger.add(filepath.ToSlash(rel), content, info.Mode().Perm())
	})
}

// localTreeDigest fingerprints a local directory source: a hash over every
// regular file's path and content hash, the local analogue of a git tree id.
func localTreeDigest(localAbs string) (string, error) {
	info, err := os.Stat(localAbs)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("not a directory: %s", localAbs)
	}
	var lines []string
	err = filepath.WalkDir(localAbs, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(localAbs, path)
		if err != nil {
			return err
		}
		hash, err := fileSHA256(path)
		if err != nil {
			return err
		}
		lines = append(lines, filepath.ToSlash(rel)+":"+hash)
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(lines)
	return sha256Hex([]byte(strings.Join(lines, "\n"))), nil
}
