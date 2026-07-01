package jig

import (
	"os/exec"
	"path/filepath"
	"testing"
)

func TestInstalledRepoIdentitySetUsesGitRepos(t *testing.T) {
	root := t.TempDir()
	model := Model{Entries: map[string]Entry{
		"observability/tracing": testRepoEntry("observability/tracing", "tracing-service", Repo{Git: "git@example.com:tracing.git"}),
	}}
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
