package jig

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

type fileSrc struct {
	GitURL string
	Path   string
}

func parseFileSrc(src string) (fileSrc, error) {
	if !strings.HasPrefix(src, "git:") {
		return fileSrc{}, errors.New("src must start with git:")
	}
	value := strings.TrimPrefix(src, "git:")
	idx := strings.LastIndex(value, "#")
	if idx <= 0 || idx == len(value)-1 {
		return fileSrc{}, errors.New("src must be git:<repo-url>#<file-path>")
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
	out, err := git(dir, "rev-parse", "HEAD:"+parsed.Path)
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
		blob, err := git(dir, "rev-parse", "HEAD:"+parsed.Path)
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
	data, err := fetchGitFileDirect(parsed.GitURL, parsed.Path)
	return data, "", err
}

// fetchGitFileDirect reads a file from the repository's default branch via a
// throwaway shallow clone, for when the cache is unavailable.
func fetchGitFileDirect(gitURL string, sourcePath string) ([]byte, error) {
	if err := validateSafePath(sourcePath); err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp("", "jig-source-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	repoDir := filepath.Join(tmp, "repo")
	if _, err := git("", "clone", "--quiet", "--depth", "1", gitURL, repoDir); err != nil {
		return nil, err
	}
	return os.ReadFile(filepath.Join(repoDir, sourcePath))
}
