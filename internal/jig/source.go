package jig

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// A src copied from a forge's web UI carries a "<ref>/<path>" tail whose
// split point depends on the repository's branch names (branches may contain
// slashes), so parsing keeps the tail in RefPath and resolveSrcPath splits it
// once a local copy of the repository is available. Path and RefPath are
// mutually exclusive.
type fileSrc struct {
	GitURL  string
	Path    string
	RefPath string
}

// forgeKind says what a forge web URL points at. Bitbucket and Gitea mark
// files and directories the same way, so theirs stay ambiguous.
type forgeKind int

const (
	forgeEither forgeKind = iota
	forgeFile
	forgeTree
)

// parseForgeSrc recognizes file and directory URLs copied from a forge's web
// UI and rewrites them to a clone URL plus an unsplit "<ref>/<path>" tail:
//
//	https://github.com/o/r/blob/<ref>/<path>          (also tree, raw)
//	https://gitlab.example.com/g/sub/r/-/blob/<ref>/<path>
//	https://bitbucket.org/o/r/src/<ref>/<path>
//	https://gitea.example.com/o/r/src/branch/<branch>/<path>
//	https://raw.githubusercontent.com/o/r/[refs/heads/]<ref>/<path>
//
// There is no cross-forge standard; these shapes cover the common hosts.
// GitLab's "-" separator and Gitea's "src/branch" marker identify those
// forges on any domain; the GitHub shape is the generic fallback.
func parseForgeSrc(src string) (fileSrc, forgeKind, bool) {
	scheme := ""
	for _, candidate := range []string{"https://", "http://"} {
		if strings.HasPrefix(src, candidate) {
			scheme = candidate
			break
		}
	}
	if scheme == "" {
		return fileSrc{}, forgeEither, false
	}
	rest := strings.TrimPrefix(src, scheme)
	// Line anchors (#L10) and view parameters (?ref_type=heads) come along
	// when copying from the browser; they never name content.
	if i := strings.IndexAny(rest, "#?"); i >= 0 {
		rest = rest[:i]
	}
	segments := strings.Split(strings.TrimSuffix(rest, "/"), "/")
	host, parts := segments[0], segments[1:]
	for _, part := range parts {
		if part == "" {
			return fileSrc{}, forgeEither, false
		}
	}

	markerKind := func(marker string) (forgeKind, bool) {
		switch marker {
		case "blob", "raw":
			return forgeFile, true
		case "tree":
			return forgeTree, true
		}
		return forgeEither, false
	}
	result := func(cloneHost string, repo []string, kind forgeKind, tail []string) (fileSrc, forgeKind, bool) {
		if len(repo) < 2 || len(tail) == 0 {
			return fileSrc{}, forgeEither, false
		}
		parsed := fileSrc{
			GitURL:  scheme + cloneHost + "/" + strings.Join(repo, "/") + ".git",
			RefPath: strings.Join(tail, "/"),
		}
		return parsed, kind, true
	}

	if host == "raw.githubusercontent.com" && len(parts) >= 4 {
		tail := parts[2:]
		if len(tail) > 2 && tail[0] == "refs" && tail[1] == "heads" {
			tail = tail[2:]
		}
		return result("github.com", parts[:2], forgeFile, tail)
	}
	// GitLab inserts "-" between the repo (possibly nested groups) and the view.
	for i := 2; i < len(parts)-1; i++ {
		if parts[i] != "-" {
			continue
		}
		if kind, ok := markerKind(parts[i+1]); ok {
			return result(host, parts[:i], kind, parts[i+2:])
		}
	}
	if host == "bitbucket.org" && len(parts) >= 4 && parts[2] == "src" {
		return result(host, parts[:2], forgeEither, parts[3:])
	}
	if len(parts) >= 5 && (parts[2] == "src" || parts[2] == "raw") &&
		(parts[3] == "branch" || parts[3] == "tag" || parts[3] == "commit") {
		return result(host, parts[:2], forgeEither, parts[4:])
	}
	if len(parts) >= 4 {
		if kind, ok := markerKind(parts[2]); ok {
			return result(host, parts[:2], kind, parts[3:])
		}
	}
	return fileSrc{}, forgeEither, false
}

// resolveSrcPath returns the in-repository path of parsed. A forge web URL
// tail is split against gitDir's default branch; matching the branch as a
// textual prefix also handles branch names containing slashes. Only
// default-branch URLs are supported, since sources always read HEAD.
func resolveSrcPath(gitDir string, parsed fileSrc) (string, error) {
	if parsed.RefPath == "" {
		return parsed.Path, nil
	}
	out, err := git(gitDir, "symbolic-ref", "--short", "HEAD")
	if err != nil {
		return "", err
	}
	branch := strings.TrimSpace(out)
	if parsed.RefPath == branch {
		return "", nil
	}
	if path, ok := strings.CutPrefix(parsed.RefPath, branch+"/"); ok {
		return path, nil
	}
	return "", fmt.Errorf("source URL does not point at the default branch %s", branch)
}

// resolveSrcFilePath is resolveSrcPath for $file sources, which must name a
// file inside the repository.
func resolveSrcFilePath(gitDir string, parsed fileSrc) (string, error) {
	path, err := resolveSrcPath(gitDir, parsed)
	if err != nil {
		return "", err
	}
	if path == "" {
		return "", errors.New("source URL does not include a file path")
	}
	return path, nil
}

// stripLegacySrcPrefix drops the historical "git:" prefix; sources are plain
// <repo-url>[#<path>]. Real git protocol URLs (git://) are left untouched.
func stripLegacySrcPrefix(src string) string {
	if strings.HasPrefix(src, "git:") && !strings.HasPrefix(src, "git://") {
		return strings.TrimPrefix(src, "git:")
	}
	return src
}

func parseFileSrc(src string) (fileSrc, error) {
	value := stripLegacySrcPrefix(src)
	if parsed, kind, ok := parseForgeSrc(value); ok {
		if kind == forgeTree {
			return fileSrc{}, errors.New("source URL points at a directory, not a file")
		}
		if err := validateSafePath(parsed.RefPath); err != nil {
			return fileSrc{}, err
		}
		return parsed, nil
	}
	idx := strings.LastIndex(value, "#")
	if idx <= 0 || idx == len(value)-1 {
		return fileSrc{}, errors.New("src must be <repo-url>#<file-path>")
	}
	parsed := fileSrc{GitURL: value[:idx], Path: value[idx+1:]}
	if strings.Contains(parsed.Path, "#") {
		return fileSrc{}, errors.New("source file path must not contain #")
	}
	if err := validateSafePath(parsed.Path); err != nil {
		return fileSrc{}, err
	}
	return parsed, nil
}

// parseDirSrc parses a $dir source: <repo-url>[#<subtree-path>]. Without a
// path the whole repository tree is materialized.
func parseDirSrc(src string) (fileSrc, error) {
	value := stripLegacySrcPrefix(src)
	if parsed, kind, ok := parseForgeSrc(value); ok {
		if kind == forgeFile {
			return fileSrc{}, errors.New("source URL points at a file, not a directory")
		}
		if err := validateSafePath(parsed.RefPath); err != nil {
			return fileSrc{}, err
		}
		return parsed, nil
	}
	idx := strings.LastIndex(value, "#")
	if idx < 0 {
		if value == "" {
			return fileSrc{}, errors.New("src must be <repo-url>[#<subtree-path>]")
		}
		return fileSrc{GitURL: value}, nil
	}
	parsed := fileSrc{GitURL: value[:idx], Path: value[idx+1:]}
	if parsed.GitURL == "" || parsed.Path == "" {
		return fileSrc{}, errors.New("src must be <repo-url>[#<subtree-path>]")
	}
	if strings.Contains(parsed.Path, "#") {
		return fileSrc{}, errors.New("source subtree path must not contain #")
	}
	if err := validateSafePath(parsed.Path); err != nil {
		return fileSrc{}, err
	}
	return parsed, nil
}

// fileFetcher reads file content and blob ids from source repositories,
// freshening each repository's cache mirror at most once per run.
type fileFetcher struct {
	mirrors map[string]mirrorResult
}

type mirrorResult struct {
	dir string
	err error
}

func newFileFetcher() *fileFetcher {
	return &fileFetcher{mirrors: map[string]mirrorResult{}}
}

func (f *fileFetcher) mirror(gitURL string) (string, error) {
	if cached, ok := f.mirrors[gitURL]; ok {
		return cached.dir, cached.err
	}
	dir, err := freshMirror(gitURL)
	f.mirrors[gitURL] = mirrorResult{dir, err}
	return dir, err
}

// prefetchMirrors freshens the mirrors of the given URLs concurrently, so
// the serial file and dir passes only hit memoized results instead of paying
// one network round-trip each. Errors are memoized too and surface through
// the existing per-entry handling.
func (f *fileFetcher) prefetchMirrors(gitURLs []string) {
	results := make([]mirrorResult, len(gitURLs))
	forEachParallel(len(gitURLs), func(i int) {
		dir, err := freshMirror(gitURLs[i])
		results[i] = mirrorResult{dir, err}
	})
	for i, gitURL := range gitURLs {
		f.mirrors[gitURL] = results[i]
	}
}

// srcBlob returns the git blob id of the source file at the repository's
// HEAD, without transferring the content.
func (f *fileFetcher) srcBlob(src string) (string, error) {
	parsed, err := parseFileSrc(src)
	if err != nil {
		return "", err
	}
	dir, err := f.mirror(parsed.GitURL)
	if err != nil {
		return "", err
	}
	path, err := resolveSrcFilePath(dir, parsed)
	if err != nil {
		return "", err
	}
	out, err := git(dir, "rev-parse", "HEAD:"+path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// content returns the source file's content and blob id. When the cache is
// unavailable it falls back to a throwaway shallow clone; the blob id is
// empty in that case.
func (f *fileFetcher) content(src string) ([]byte, string, error) {
	parsed, err := parseFileSrc(src)
	if err != nil {
		return nil, "", err
	}
	dir, mirrorErr := f.mirror(parsed.GitURL)
	if mirrorErr == nil {
		path, err := resolveSrcFilePath(dir, parsed)
		if err != nil {
			return nil, "", err
		}
		blob, err := git(dir, "rev-parse", "HEAD:"+path)
		if err != nil {
			return nil, "", err
		}
		blob = strings.TrimSpace(blob)
		out, err := git(dir, "cat-file", "blob", blob)
		if err != nil {
			return nil, "", err
		}
		return []byte(out), blob, nil
	}
	data, err := fetchGitFileDirect(parsed)
	return data, "", err
}

// fetchGitFileDirect reads a file from the repository's default branch via a
// throwaway shallow clone, for when the cache is unavailable.
func fetchGitFileDirect(parsed fileSrc) ([]byte, error) {
	tmp, err := os.MkdirTemp("", "jig-source-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	repoDir := filepath.Join(tmp, "repo")
	if _, err := git("", "clone", "--quiet", "--depth", "1", parsed.GitURL, repoDir); err != nil {
		return nil, err
	}
	sourcePath, err := resolveSrcFilePath(repoDir, parsed)
	if err != nil {
		return nil, err
	}
	if err := validateSafePath(sourcePath); err != nil {
		return nil, err
	}
	return os.ReadFile(filepath.Join(repoDir, sourcePath))
}
