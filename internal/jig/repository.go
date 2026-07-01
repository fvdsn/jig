package jig

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func ensureRepo(out io.Writer, root string, model *Model, state *State, repoPath string, allowMove bool) error {
	entry, _ := model.entry(repoPath, EntryRepo)
	repo := entry.Repo
	expectedRel := entry.Path
	expectedAbs := filepath.Join(root, expectedRel)
	stateRepo, hasState := state.Repos[entry.Identity]

	if hasState && stateRepo.Path != expectedRel {
		oldAbs := filepath.Join(root, stateRepo.Path)
		if isGitRepo(oldAbs) {
			if !allowMove {
				return fmt.Errorf("already installed at %s; run jig sync to move it", stateRepo.Path)
			}
			if pathExists(expectedAbs) {
				return fmt.Errorf("target path already exists: %s", expectedRel)
			}
			if isDirty(oldAbs) {
				return fmt.Errorf("repository has uncommitted changes and would need to be moved")
			}
			if err := os.MkdirAll(filepath.Dir(expectedAbs), 0o755); err != nil {
				return err
			}
			if err := os.Rename(oldAbs, expectedAbs); err != nil {
				return err
			}
			pruneEmptyParents(root, filepath.Dir(stateRepo.Path))
			fmt.Fprintf(out, "moved: %s: %s -> %s\n", repoPath, stateRepo.Path, expectedRel)
			stateRepo.Path = expectedRel
			state.Repos[entry.Identity] = stateRepo
			hasState = true
		} else {
			delete(state.Repos, entry.Identity)
			hasState = false
		}
	}

	if !pathExists(expectedAbs) {
		if err := os.MkdirAll(filepath.Dir(expectedAbs), 0o755); err != nil {
			return err
		}
		if _, err := git("", "clone", repo.Git, expectedAbs); err != nil {
			return err
		}
		state.Repos[entry.Identity] = StateRepo{Path: expectedRel, Git: repo.Git}
		fmt.Fprintf(out, "cloned: %s\n", repoPath)
		return nil
	}

	if !isGitRepo(expectedAbs) {
		return fmt.Errorf("expected path exists and is not a Git repository: %s", expectedRel)
	}
	origin, err := gitOrigin(expectedAbs)
	if err != nil {
		return err
	}
	if origin != repo.Git {
		if allowMove && hasState && state.Repos[entry.Identity].Path == expectedRel {
			if _, err := git(expectedAbs, "remote", "set-url", "origin", repo.Git); err != nil {
				return err
			}
			state.Repos[entry.Identity] = StateRepo{Path: expectedRel, Git: repo.Git}
			fmt.Fprintf(out, "updated-origin: %s\n", repoPath)
			return nil
		}
		return fmt.Errorf("existing Git repository has different origin at %s", expectedRel)
	}
	state.Repos[entry.Identity] = StateRepo{Path: expectedRel, Git: repo.Git}
	fmt.Fprintf(out, "present: %s\n", repoPath)
	return nil
}

func installedDefinedRepos(root string, model *Model, state *State) []string {
	identityToPath := repoIdentityToPath(model)
	resultSet := map[string]bool{}
	for identity, stateRepo := range state.Repos {
		repoPath, ok := identityToPath[identity]
		if !ok {
			continue
		}
		if isGitRepo(filepath.Join(root, stateRepo.Path)) {
			resultSet[repoPath] = true
		}
	}
	for _, repoPath := range sortedRepoPaths(model) {
		if isGitRepo(filepath.Join(root, repoPath)) {
			resultSet[repoPath] = true
		}
	}
	return sortedKeys(resultSet)
}

func installedPath(root string, model *Model, state *State, repoPath string) (string, bool) {
	entry, _ := model.entry(repoPath, EntryRepo)
	if stateRepo, ok := state.Repos[entry.Identity]; ok {
		abs := filepath.Join(root, stateRepo.Path)
		if isGitRepo(abs) {
			return abs, true
		}
	}
	expected := filepath.Join(root, entry.Path)
	if isGitRepo(expected) {
		return expected, true
	}
	return "", false
}

func installedRepoIdentitySet(root string, model *Model, state *State) map[string]bool {
	installed := map[string]bool{}
	identityToPath := repoIdentityToPath(model)
	for identity, stateRepo := range state.Repos {
		if _, ok := identityToPath[identity]; !ok {
			continue
		}
		if isGitRepo(filepath.Join(root, stateRepo.Path)) {
			installed[identity] = true
		}
	}
	for _, repoPath := range sortedRepoPaths(model) {
		entry, _ := model.entry(repoPath, EntryRepo)
		if isGitRepo(filepath.Join(root, entry.Path)) {
			installed[entry.Identity] = true
		}
	}
	return installed
}

func isGitRepo(path string) bool {
	// A checked-out repo has its own .git at its root (a directory for a normal
	// clone, a file for worktrees/submodules). Checking for it directly avoids
	// forking a git process per candidate path, which is costly when scanning a
	// workspace with hundreds of repos.
	return pathExists(filepath.Join(path, ".git"))
}

func gitOrigin(path string) (string, error) {
	out, err := git(path, "remote", "get-url", "origin")
	return strings.TrimSpace(out), err
}

// gitBranch returns the current branch name, or a short "@<sha>" for a
// detached HEAD, or "" if neither can be determined.
func gitBranch(path string) string {
	if out, err := git(path, "branch", "--show-current"); err == nil {
		if branch := strings.TrimSpace(out); branch != "" {
			return branch
		}
	}
	if out, err := git(path, "rev-parse", "--short", "HEAD"); err == nil {
		if sha := strings.TrimSpace(out); sha != "" {
			return "@" + sha
		}
	}
	return ""
}

func isDirty(path string) bool {
	out, err := git(path, "status", "--porcelain")
	return err == nil && strings.TrimSpace(out) != ""
}

func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return stdout.String(), errors.New(msg)
	}
	return stdout.String(), nil
}
