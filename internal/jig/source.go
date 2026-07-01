package jig

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
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

func discoverDefaultBranch(gitURL string) (string, error) {
	out, err := exec.Command("git", "ls-remote", "--symref", gitURL, "HEAD").Output()
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "ref: refs/heads/") {
			continue
		}
		line = strings.TrimPrefix(line, "ref: refs/heads/")
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		return fields[0], nil
	}
	return "", errors.New("default branch not found")
}

func fetchDefinition(gitURL, ref, definitionPath string) (*Definition, error) {
	data, err := fetchGitFile(gitURL, ref, definitionPath)
	if err != nil {
		return nil, err
	}
	var def Definition
	if err := json.Unmarshal(data, &def); err != nil {
		return nil, err
	}
	return &def, nil
}

func fetchGitFile(gitURL, ref, sourcePath string) ([]byte, error) {
	if sourcePath == "" {
		sourcePath = definitionFile
	}
	if err := validateSafePath(sourcePath); err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp("", "jig-source-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	repoDir := filepath.Join(tmp, "repo")
	args := []string{"clone", "--quiet", "--depth", "1"}
	if ref != "" {
		args = append(args, "--branch", ref, "--single-branch")
	}
	args = append(args, gitURL, repoDir)
	if _, err := git("", args...); err != nil {
		return nil, err
	}
	return os.ReadFile(filepath.Join(repoDir, sourcePath))
}
