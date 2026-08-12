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

func TestGitOriginIgnoresInsteadOfRewrites(t *testing.T) {
	root := t.TempDir()
	gitIn(t, root, "init", "-q")
	gitIn(t, root, "remote", "add", "origin", "git@example.com:org/repo.git")
	// A machine-local URL rewrite must not leak into origin comparisons:
	// the schema URL is the stored one, not its rewritten form.
	gitIn(t, root, "config", "url.git@alias:org/.insteadOf", "git@example.com:org/")

	origin, err := gitOrigin(root)
	if err != nil {
		t.Fatal(err)
	}
	if origin != "git@example.com:org/repo.git" {
		t.Fatalf("origin = %q, want the configured URL, not the rewrite", origin)
	}
}
