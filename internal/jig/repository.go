package jig

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ensureRepoResult carries the outcome of ensureRepo so state and output
// updates can be applied serially after concurrent runs. State changes and
// messages accumulated before Err are still valid and must be applied.
type ensureRepoResult struct {
	StateRepo *StateRepo // record for the identity when non-nil
	Remove    bool       // drop the identity from state
	Messages  []string
	Err       error
}

func ensureRepo(root string, entry Entry, stateRepo StateRepo, hasState bool, allowMove bool) ensureRepoResult {
	repo := entry.Repo
	expectedRel := entry.Path
	expectedAbs := filepath.Join(root, expectedRel)
	var result ensureRepoResult

	if hasState && stateRepo.Path != expectedRel {
		oldAbs := filepath.Join(root, stateRepo.Path)
		if isGitRepo(oldAbs) {
			if !allowMove {
				result.Err = fmt.Errorf("already installed at %s; run jig sync to move it", stateRepo.Path)
				return result
			}
			if isDirty(oldAbs) {
				result.Err = fmt.Errorf("repository has uncommitted changes and would need to be moved")
				return result
			}
			message, err := moveInstalledPath(root, entry.Path, stateRepo.Path, expectedRel, "moved")
			if err != nil {
				result.Err = err
				return result
			}
			result.Messages = append(result.Messages, message)
			stateRepo.Path = expectedRel
			moved := stateRepo
			result.StateRepo = &moved
		} else {
			result.Remove = true
			hasState = false
		}
	}

	if !pathExists(expectedAbs) {
		if err := os.MkdirAll(filepath.Dir(expectedAbs), 0o755); err != nil {
			result.Err = err
			return result
		}
		if _, err := git("", "clone", repo.Git, expectedAbs); err != nil {
			result.Err = err
			return result
		}
		result.StateRepo = &StateRepo{Path: expectedRel, Git: repo.Git}
		result.Messages = append(result.Messages, "cloned: "+entry.Path)
		return result
	}

	if !isGitRepo(expectedAbs) {
		result.Err = fmt.Errorf("expected path exists and is not a Git repository: %s", expectedRel)
		return result
	}
	origin, err := gitOrigin(expectedAbs)
	if err != nil {
		result.Err = err
		return result
	}
	if origin != repo.Git {
		if allowMove && hasState && stateRepo.Path == expectedRel {
			if _, err := git(expectedAbs, "remote", "set-url", "origin", repo.Git); err != nil {
				result.Err = err
				return result
			}
			result.StateRepo = &StateRepo{Path: expectedRel, Git: repo.Git}
			result.Messages = append(result.Messages, "updated-origin: "+entry.Path)
			return result
		}
		result.Err = fmt.Errorf("existing Git repository has different origin at %s", expectedRel)
		return result
	}
	result.StateRepo = &StateRepo{Path: expectedRel, Git: repo.Git}
	result.Messages = append(result.Messages, "present: "+entry.Path)
	return result
}

func installedDefinedRepos(root string, model *Model, state *State) []string {
	identityToPath := repoIdentityToPath(model)
	resultSet := map[string]bool{}
	for identity := range installedRepoIdentitySet(root, model, state) {
		resultSet[identityToPath[identity]] = true
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
