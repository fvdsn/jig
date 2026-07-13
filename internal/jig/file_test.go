package jig

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestFileLinkValidationAndOrdering(t *testing.T) {
	def := testDefinition(t, `{
  "version": 1,
  "tree": {
    "scripts/dev.sh": {
      "$file": {
        "id": "dev-script",
        "src": "git:git@example.com:config.git#scripts/dev.sh"
      }
    },
    "bin/dev": {
      "$file": {
        "id": "dev-command",
        "link": "scripts/dev.sh"
      }
    }
  }
}`)
	result := validateDefinition(def)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected validation errors: %#v", result.Errors)
	}
	model, err := flattenDefinition(def)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := resolvePlan(&model, []string{}, planOptions{})
	if err != nil {
		t.Fatal(err)
	}
	active := map[string]bool{"scripts/dev.sh": true, "bin/dev": true}
	ordered := orderFilesForApply(&model, active)
	want := []string{"scripts/dev.sh", "bin/dev"}
	if !reflect.DeepEqual(ordered, want) {
		t.Fatalf("ordered files = %#v, want %#v", ordered, want)
	}
	_ = plan
}

func TestEnsureLinkFileCreatesRelativeSymlink(t *testing.T) {
	root := t.TempDir()
	targetPath := filepath.Join(root, "scripts", "dev.sh")
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targetPath, []byte("script"), 0o644); err != nil {
		t.Fatal(err)
	}
	state := emptyState()
	model := Model{Entries: map[string]Entry{
		"scripts/dev.sh": testFileEntry("scripts/dev.sh", "dev-script", File{Src: SrcList{{Src: "git:git@example.com:config.git#scripts/dev.sh"}}}),
		"bin/dev":        testFileEntry("bin/dev", "dev-command", File{Link: "scripts/dev.sh"}),
	}}

	if err := ensureFile(ioDiscard{}, root, &model, &state, "bin/dev", true, newFileFetcher(), nil, nil); err != nil {
		t.Fatal(err)
	}
	target, err := os.Readlink(filepath.Join(root, "bin", "dev"))
	if err != nil {
		t.Fatal(err)
	}
	if target != "../scripts/dev.sh" {
		t.Fatalf("symlink target = %q", target)
	}
	if state.Files["dev-command"].Link != "scripts/dev.sh" {
		t.Fatalf("state = %#v", state.Files["dev-command"])
	}
}

func TestEnsureFilePreservesLocalModification(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "scripts", "dev.sh")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("edited"), 0o644); err != nil {
		t.Fatal(err)
	}
	state := State{Version: 1, Repos: map[string]StateRepo{}, Files: map[string]StateFile{
		"dev-script": {Path: "scripts/dev.sh", Src: "git:git@example.com:config.git#scripts/dev.sh", SHA256: sha256Hex([]byte("original"))},
	}}
	model := Model{Entries: map[string]Entry{
		"scripts/dev.sh": testFileEntry("scripts/dev.sh", "dev-script", File{Src: SrcList{{Src: "git:git@example.com:config.git#scripts/dev.sh"}}}),
	}}

	err := ensureFile(ioDiscard{}, root, &model, &state, "scripts/dev.sh", true, newFileFetcher(), nil, nil)
	if err == nil || err.Error() != "locally modified" {
		t.Fatalf("expected locally modified error, got %v", err)
	}
}

func TestEnsureFilePicksUpSourceChanges(t *testing.T) {
	root := t.TempDir()
	t.Setenv("JIG_CACHE_DIR", filepath.Join(root, "cache"))
	remote := filepath.Join(root, "remote")
	testRemoteRepo(t, remote)
	src := "git:" + remote + "#README.md"

	state := emptyState()
	model := Model{Entries: map[string]Entry{
		"docs/readme.md": testFileEntry("docs/readme.md", "readme", File{Src: SrcList{{Src: src}}}),
	}}
	ensure := func() string {
		var out bytes.Buffer
		if err := ensureFile(&out, root, &model, &state, "docs/readme.md", true, newFileFetcher(), nil, nil); err != nil {
			t.Fatalf("ensureFile: %v", err)
		}
		return out.String()
	}

	if got := ensure(); !strings.Contains(got, "wrote-file:") {
		t.Fatalf("first run = %q, want wrote-file", got)
	}
	if state.Files["readme"].SrcBlob == "" {
		t.Fatal("expected srcBlob recorded")
	}
	if got := ensure(); !strings.Contains(got, "present-file:") {
		t.Fatalf("second run = %q, want present-file", got)
	}

	// Change the file upstream: sync must pick it up.
	if err := os.WriteFile(filepath.Join(remote, "README.md"), []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	commit := exec.Command("git", "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qam", "v2")
	commit.Dir = remote
	if out, err := commit.CombinedOutput(); err != nil {
		t.Fatalf("commit: %v\n%s", err, out)
	}
	if got := ensure(); !strings.Contains(got, "updated-file:") {
		t.Fatalf("after upstream change = %q, want updated-file", got)
	}
	data, err := os.ReadFile(filepath.Join(root, "docs", "readme.md"))
	if err != nil || string(data) != "v2\n" {
		t.Fatalf("content = %q, %v; want v2", data, err)
	}

	// A locally modified file is never overwritten, even when upstream moved.
	if err := os.WriteFile(filepath.Join(root, "docs", "readme.md"), []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err = ensureFile(ioDiscard{}, root, &model, &state, "docs/readme.md", true, newFileFetcher(), nil, nil)
	if err == nil || err.Error() != "locally modified" {
		t.Fatalf("expected locally modified error, got %v", err)
	}
}

// testFileSource creates a git repository holding the given files, for use
// as a $file source.
func testFileSource(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitIn(t, dir, "init", "-q")
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-qm", "init")
}

func TestEnsureFileConcatenatesMultipleSources(t *testing.T) {
	root := t.TempDir()
	t.Setenv("JIG_CACHE_DIR", filepath.Join(root, "cache"))
	source := filepath.Join(root, "config")
	// The first part misses its trailing newline: the join inserts one.
	testFileSource(t, source, map[string]string{
		"agents/base.md":  "base",
		"agents/extra.md": "extra\n",
	})

	state := emptyState()
	model := Model{Entries: map[string]Entry{
		"AGENTS.md": testFileEntry("AGENTS.md", "agents", File{Src: SrcList{
			{Src: source + "#agents/base.md"},
			{Src: source + "#agents/extra.md"},
		}}),
	}}
	ensure := func() string {
		var out bytes.Buffer
		if err := ensureFile(&out, root, &model, &state, "AGENTS.md", true, newFileFetcher(), nil, nil); err != nil {
			t.Fatalf("ensureFile: %v", err)
		}
		return out.String()
	}

	if got := ensure(); !strings.Contains(got, "wrote-file:") {
		t.Fatalf("first run = %q, want wrote-file", got)
	}
	data, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil || string(data) != "base\nextra\n" {
		t.Fatalf("content = %q, %v; want base and extra joined by a newline", data, err)
	}
	if !strings.Contains(state.Files["agents"].SrcBlob, "+") {
		t.Fatalf("srcBlob = %q, want combined blob ids", state.Files["agents"].SrcBlob)
	}
	if got := ensure(); !strings.Contains(got, "present-file:") {
		t.Fatalf("second run = %q, want present-file", got)
	}

	// An update in the second source flows through the concatenation.
	if err := os.WriteFile(filepath.Join(source, "agents", "extra.md"), []byte("extra v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, source, "commit", "-qam", "v2")
	if got := ensure(); !strings.Contains(got, "updated-file:") {
		t.Fatalf("update run = %q, want updated-file", got)
	}
	if data, _ := os.ReadFile(filepath.Join(root, "AGENTS.md")); string(data) != "base\nextra v2\n" {
		t.Fatalf("content = %q, want updated concatenation", data)
	}
}

func TestFileSourcesGatedByOnlyWhen(t *testing.T) {
	root := t.TempDir()
	t.Setenv("JIG_CACHE_DIR", filepath.Join(root, "cache"))
	source := filepath.Join(root, "config")
	testFileSource(t, source, map[string]string{
		"agents/base.md":    "base\n",
		"agents/billing.md": "billing\n",
	})

	state := emptyState()
	model := Model{Entries: map[string]Entry{
		"billing/api": testRepoEntry("billing/api", "billing-api", Repo{Git: "git@example.com:billing.git"}),
		"AGENTS.md": testFileEntry("AGENTS.md", "agents", File{Src: SrcList{
			{Src: source + "#agents/base.md"},
			{Src: source + "#agents/billing.md", OnlyWhen: &Condition{Path: "billing"}},
		}}),
	}}
	ensure := func(activeRepos map[string]bool) string {
		var out bytes.Buffer
		if err := ensureFile(&out, root, &model, &state, "AGENTS.md", true, newFileFetcher(), activeRepos, nil); err != nil {
			t.Fatalf("ensureFile: %v", err)
		}
		return out.String()
	}
	content := func() string {
		data, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}

	// Without billing active, only the base section is written.
	ensure(nil)
	if content() != "base\n" {
		t.Fatalf("content = %q, want base only", content())
	}

	// Activating billing appends the gated section.
	if got := ensure(map[string]bool{"billing/api": true}); !strings.Contains(got, "updated-file:") {
		t.Fatalf("activation run = %q, want updated-file", got)
	}
	if content() != "base\nbilling\n" {
		t.Fatalf("content = %q, want base and billing", content())
	}

	// Deactivating drops it again.
	ensure(nil)
	if content() != "base\n" {
		t.Fatalf("content = %q, want base only after deactivation", content())
	}
}

func TestFileWithoutActiveSourcesIsNotGenerated(t *testing.T) {
	root := t.TempDir()
	t.Setenv("JIG_CACHE_DIR", filepath.Join(root, "cache"))
	source := filepath.Join(root, "config")
	testFileSource(t, source, map[string]string{"agents/billing.md": "billing\n"})

	state := emptyState()
	model := Model{Entries: map[string]Entry{
		"billing/api": testRepoEntry("billing/api", "billing-api", Repo{Git: "git@example.com:billing.git"}),
		"AGENTS.md": testFileEntry("AGENTS.md", "agents", File{Src: SrcList{
			{Src: source + "#agents/billing.md", OnlyWhen: &Condition{Path: "billing"}},
		}}),
	}}
	ensure := func(activeRepos map[string]bool) string {
		var out bytes.Buffer
		if err := ensureFile(&out, root, &model, &state, "AGENTS.md", true, newFileFetcher(), activeRepos, nil); err != nil {
			t.Fatalf("ensureFile: %v", err)
		}
		return out.String()
	}

	// With every source gated off, no file appears.
	if got := ensure(nil); !strings.Contains(got, "inactive-file:") {
		t.Fatalf("inactive run = %q, want inactive-file", got)
	}
	if pathExists(filepath.Join(root, "AGENTS.md")) {
		t.Fatal("did not expect AGENTS.md without active sources")
	}

	// Deactivating every source of a written untouched file removes it.
	ensure(map[string]bool{"billing/api": true})
	if !pathExists(filepath.Join(root, "AGENTS.md")) {
		t.Fatal("expected AGENTS.md with billing active")
	}
	if got := ensure(nil); !strings.Contains(got, "removed-file:") {
		t.Fatalf("deactivation run = %q, want removed-file", got)
	}
	if pathExists(filepath.Join(root, "AGENTS.md")) {
		t.Fatal("expected AGENTS.md removed after deactivation")
	}
	if _, tracked := state.Files["agents"]; tracked {
		t.Fatalf("expected state entry dropped, got %#v", state.Files["agents"])
	}

	// A locally modified file is never deleted; it is left untracked.
	ensure(map[string]bool{"billing/api": true})
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ensure(nil); !strings.Contains(got, "left untracked") {
		t.Fatalf("modified deactivation run = %q, want left untracked", got)
	}
	if data, _ := os.ReadFile(filepath.Join(root, "AGENTS.md")); string(data) != "edited\n" {
		t.Fatalf("content = %q, want local edit kept", data)
	}
	if _, tracked := state.Files["agents"]; tracked {
		t.Fatal("expected state entry dropped for the abandoned file")
	}
}

func TestFileSrcListValidation(t *testing.T) {
	good := testDefinition(t, `{
  "version": 1,
  "tree": {
    "billing/api": { "$repo": { "git": "git@example.com:billing.git" } },
    "AGENTS.md": {
      "$file": {
        "src": [
          "git@example.com:config.git#agents/base.md",
          { "src": "git@example.com:config.git#agents/billing.md",
            "onlyWhen": { "path": "billing" } }
        ]
      }
    }
  }
}`)
	if result := validateDefinition(good); len(result.Errors) > 0 {
		t.Fatalf("unexpected validation errors: %#v", result.Errors)
	}

	bad := testDefinition(t, `{
  "version": 1,
  "tree": {
    "billing/api": { "$repo": { "git": "git@example.com:billing.git" } },
    "AGENTS.md": {
      "$file": {
        "src": [
          "git@example.com:config.git",
          { "src": "git@example.com:config.git#agents/billing.md",
            "onlyWhen": { "path": "nope" } }
        ]
      }
    }
  }
}`)
	result := validateDefinition(bad)
	if len(result.Errors) != 2 {
		t.Fatalf("errors = %#v, want invalid src and unsatisfiable onlyWhen", result.Errors)
	}
	if !strings.Contains(result.Errors[0], "invalid src") {
		t.Fatalf("first error = %q, want invalid src", result.Errors[0])
	}
	if !strings.Contains(result.Errors[1], "does not match any repository") {
		t.Fatalf("second error = %q, want unsatisfiable onlyWhen", result.Errors[1])
	}
}

func TestInstalledFileIdentitySetRequiresTrackedExistingFile(t *testing.T) {
	root := t.TempDir()
	model := Model{
		Entries: map[string]Entry{
			"tracked.txt":   testFileEntry("tracked.txt", "tracked", File{Archived: true}),
			"untracked.txt": testFileEntry("untracked.txt", "untracked", File{Archived: true}),
		},
	}
	state := State{
		Version: 1,
		Repos:   map[string]StateRepo{},
		Files: map[string]StateFile{
			"tracked": {Path: "tracked.txt"},
		},
	}
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("tracked"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "untracked.txt"), []byte("untracked"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := installedFileIdentitySet(root, &model, &state)
	if !got["tracked"] {
		t.Fatalf("expected tracked file to be installed: %#v", got)
	}
	if got["untracked"] {
		t.Fatalf("did not expect untracked file to be installed: %#v", got)
	}
}
