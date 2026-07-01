package jig

import (
	"os/exec"
	"path/filepath"
	"testing"
)

func TestInstalledRepoIdentitySetUsesGitRepos(t *testing.T) {
	root := t.TempDir()
	model := Model{Repos: map[string]RepoEntry{
		"observability/tracing": {Path: "observability/tracing", Identity: "tracing-service", Repo: Repo{Git: "git@example.com:tracing.git"}},
	}, Files: map[string]FileEntry{}}
	state := State{Version: 1, Repos: map[string]StateRepo{
		"tracing-service": {Path: "observability/tracing", Git: "git@example.com:tracing.git"},
	}, Files: map[string]StateFile{}}

	repoDir := filepath.Join(root, "observability", "tracing")
	if err := exec.Command("git", "init", repoDir).Run(); err != nil {
		t.Fatal(err)
	}

	got := installedRepoIdentitySet(root, &model, &state)
	if !got["tracing-service"] {
		t.Fatalf("expected tracing-service to be installed: %#v", got)
	}
}
