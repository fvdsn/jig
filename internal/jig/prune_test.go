package jig

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadoptRenamedIdentities(t *testing.T) {
	model := Model{Entries: map[string]Entry{
		"services/a": testRepoEntry("services/a", "new-id", Repo{Git: "git@example.com:a.git"}),
		"dev.sh":     testFileEntry("dev.sh", "new-script", File{Src: "git@example.com:config.git#dev.sh"}),
	}}
	state := emptyState()
	state.Repos["old-id"] = StateRepo{Path: "services/a", Git: "git@example.com:a.git"}
	state.Files["old-script"] = StateFile{Path: "dev.sh", Src: "git@example.com:config.git#dev.sh", SHA256: "abc"}
	// A record at another path is unrelated and must stay untouched.
	state.Repos["elsewhere"] = StateRepo{Path: "services/b", Git: "git@example.com:b.git"}

	var out bytes.Buffer
	readoptRenamedIdentities(&out, &model, &state)

	if _, ok := state.Repos["old-id"]; ok {
		t.Fatal("expected old repo identity to be dropped")
	}
	if record, ok := state.Repos["new-id"]; !ok || record.Path != "services/a" || record.Git != "git@example.com:a.git" {
		t.Fatalf("repo record not transferred: %#v", state.Repos)
	}
	if record, ok := state.Files["new-script"]; !ok || record.SHA256 != "abc" {
		t.Fatalf("file record not transferred: %#v", state.Files)
	}
	if _, ok := state.Repos["elsewhere"]; !ok {
		t.Fatal("unrelated record must not be touched")
	}
	if !strings.Contains(out.String(), "readopted:") {
		t.Fatalf("expected readopted report, got %q", out.String())
	}

	// A leftover duplicate (the new identity was already adopted at the
	// same path) is dropped without clobbering the adopted record.
	state.Repos["stale-dup"] = StateRepo{Path: "services/a", Git: "git@example.com:a.git"}
	readoptRenamedIdentities(ioDiscard{}, &model, &state)
	if _, ok := state.Repos["stale-dup"]; ok {
		t.Fatal("expected duplicate record to be dropped")
	}
	if record := state.Repos["new-id"]; record.Path != "services/a" {
		t.Fatalf("adopted record clobbered: %#v", record)
	}
}

func TestSyncPruneDeletesStaleEntriesSafely(t *testing.T) {
	root := t.TempDir()
	remoteKeep := filepath.Join(root, "remote-keep")
	remoteStale := filepath.Join(root, "remote-stale")
	remoteDirty := filepath.Join(root, "remote-dirty")
	testRemoteRepo(t, remoteKeep)
	testRemoteRepo(t, remoteStale)
	testRemoteRepo(t, remoteDirty)
	writeTestWorkspace(t, root, fmt.Sprintf(`{
  "version": 1,
  "tree": {
    "services/keep": { "$repo": { "id": "keep", "git": %q } }
  }
}`, remoteKeep))

	for path, remote := range map[string]string{
		"services/keep":  remoteKeep,
		"services/stale": remoteStale,
		"services/dirty": remoteDirty,
	} {
		gitIn(t, root, "clone", "-q", remote, filepath.Join(root, filepath.FromSlash(path)))
	}
	if err := os.WriteFile(filepath.Join(root, "services", "dirty", "edit.txt"), []byte("wip\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	staleContent := []byte("notes\n")
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), staleContent, 0o644); err != nil {
		t.Fatal(err)
	}

	state := emptyState()
	state.Repos["keep"] = StateRepo{Path: "services/keep", Git: remoteKeep}
	state.Repos["stale"] = StateRepo{Path: "services/stale", Git: remoteStale}
	state.Repos["dirty"] = StateRepo{Path: "services/dirty", Git: remoteDirty}
	state.Files["notes"] = StateFile{Path: "notes.txt", Src: "git@example.com:config.git#notes.txt", SHA256: sha256Hex(staleContent)}
	if err := saveState(root, state); err != nil {
		t.Fatal(err)
	}

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(oldWd); err != nil {
			t.Fatal(err)
		}
	}()

	var out bytes.Buffer
	if err := Sync(SyncOptions{Prune: true}, &out); err != nil {
		t.Fatalf("sync --prune: %v\n%s", err, out.String())
	}
	got := out.String()

	if pathExists(filepath.Join(root, "services", "stale")) {
		t.Fatalf("expected stale repo to be deleted:\n%s", got)
	}
	if pathExists(filepath.Join(root, "notes.txt")) {
		t.Fatalf("expected stale file to be deleted:\n%s", got)
	}
	if !pathExists(filepath.Join(root, "services", "dirty", "edit.txt")) {
		t.Fatalf("expected dirty repo to be kept:\n%s", got)
	}
	if !pathExists(filepath.Join(root, "services", "keep", "README.md")) {
		t.Fatalf("expected defined repo to be kept:\n%s", got)
	}
	if !strings.Contains(got, "kept:") || !strings.Contains(got, "services/dirty: uncommitted changes") {
		t.Fatalf("expected dirty repo reported kept, got:\n%s", got)
	}

	loaded, err := loadState(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := loaded.Repos["stale"]; ok {
		t.Fatal("expected stale repo state to be dropped")
	}
	if _, ok := loaded.Files["notes"]; ok {
		t.Fatal("expected stale file state to be dropped")
	}
	if _, ok := loaded.Repos["dirty"]; !ok {
		t.Fatal("expected dirty repo to stay tracked")
	}

	// A renamed id at the same path is readopted, never pruned.
	writeTestWorkspace2 := fmt.Sprintf(`{
  "version": 1,
  "tree": {
    "services/keep": { "$repo": { "id": "keep-renamed", "git": %q } }
  }
}`, remoteKeep)
	if err := os.WriteFile(filepath.Join(root, sourceDir, "jig.json"), []byte(writeTestWorkspace2), 0o644); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := Sync(SyncOptions{Prune: true}, &out); err != nil {
		t.Fatalf("sync after rename: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "readopted:") {
		t.Fatalf("expected readoption, got:\n%s", out.String())
	}
	if !pathExists(filepath.Join(root, "services", "keep", "README.md")) {
		t.Fatal("renamed repo must not be deleted")
	}
	loaded, err = loadState(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := loaded.Repos["keep-renamed"]; !ok {
		t.Fatalf("expected state under the new identity: %#v", loaded.Repos)
	}
	if _, ok := loaded.Repos["keep"]; ok {
		t.Fatal("expected old identity to be gone")
	}
}
