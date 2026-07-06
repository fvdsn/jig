package jig

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.email=t@t", "-c", "user.name=t"}, args...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestEnsureDirLifecycle(t *testing.T) {
	root := t.TempDir()
	t.Setenv("JIG_CACHE_DIR", filepath.Join(root, "cache"))
	remote := filepath.Join(root, "remote")
	if err := os.MkdirAll(filepath.Join(remote, "scripts", "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(rel, content string, mode os.FileMode) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(remote, rel), []byte(content), mode); err != nil {
			t.Fatal(err)
		}
	}
	write("scripts/dev.sh", "#!/bin/sh\necho dev\n", 0o755)
	write("scripts/sub/util.sh", "util v1\n", 0o644)
	write("scripts/gone.sh", "gone\n", 0o644)
	write("top.txt", "top\n", 0o644)
	gitIn(t, remote, "init", "-q")
	gitIn(t, remote, "add", ".")
	gitIn(t, remote, "commit", "-qm", "init")

	state := emptyState()
	model := Model{Entries: map[string]Entry{
		"tools/scripts": {Path: "tools/scripts", Identity: "scripts", Kind: EntryDir,
			Dir: &Dir{Src: SrcList{{Src: remote + "#scripts"}}}},
	}}
	ensure := func() string {
		var out bytes.Buffer
		if err := ensureDir(&out, root, &model, &state, "tools/scripts", true, false, newFileFetcher(), nil, nil); err != nil {
			t.Fatalf("ensureDir: %v", err)
		}
		return out.String()
	}

	// Initial materialization.
	if got := ensure(); !strings.Contains(got, "wrote-dir: tools/scripts (3 added)") {
		t.Fatalf("first run = %q", got)
	}
	devPath := filepath.Join(root, "tools", "scripts", "dev.sh")
	if info, err := os.Stat(devPath); err != nil || info.Mode().Perm() != 0o755 {
		t.Fatalf("dev.sh mode = %v, %v; want 0755", info, err)
	}
	if got := ensure(); !strings.Contains(got, "present-dir:") {
		t.Fatalf("second run = %q", got)
	}

	// Local modification survives an upstream update of another file, and a
	// file deleted upstream is removed locally when untouched.
	if err := os.WriteFile(filepath.Join(root, "tools", "scripts", "sub", "util.sh"), []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	write("scripts/dev.sh", "#!/bin/sh\necho dev v2\n", 0o755)
	write("scripts/sub/util.sh", "util v2\n", 0o644)
	write("scripts/new.sh", "new\n", 0o644)
	if err := os.Remove(filepath.Join(remote, "scripts", "gone.sh")); err != nil {
		t.Fatal(err)
	}
	gitIn(t, remote, "add", "-A")
	gitIn(t, remote, "commit", "-qm", "v2")

	got := ensure()
	for _, want := range []string{"updated-dir:", "1 added", "1 updated", "1 deleted", "1 modified kept"} {
		if !strings.Contains(got, want) {
			t.Fatalf("update run = %q, missing %q", got, want)
		}
	}
	if data, _ := os.ReadFile(devPath); string(data) != "#!/bin/sh\necho dev v2\n" {
		t.Fatalf("dev.sh = %q, want v2", data)
	}
	if data, _ := os.ReadFile(filepath.Join(root, "tools", "scripts", "sub", "util.sh")); string(data) != "edited\n" {
		t.Fatalf("util.sh = %q, want local edit kept", data)
	}
	if pathExists(filepath.Join(root, "tools", "scripts", "gone.sh")) {
		t.Fatal("expected gone.sh deleted")
	}
	if !pathExists(filepath.Join(root, "tools", "scripts", "new.sh")) {
		t.Fatal("expected new.sh written")
	}
}

func TestDirValidationAndWholeRepoSrc(t *testing.T) {
	def := testDefinition(t, `{
  "version": 1,
  "tree": {
    "tools/config": {
      "$dir": { "id": "config", "src": "git:git@example.com:config.git" }
    },
    "tools/bad": {
      "$dir": { "id": "bad", "src": "git@example.com:config.git#" }
    }
  }
}`)
	result := validateDefinition(def)
	if len(result.Errors) != 1 || !strings.Contains(result.Errors[0], "tools/bad") {
		t.Fatalf("errors = %#v, want one about tools/bad", result.Errors)
	}
}

func TestEnsureDirMergesMultipleSources(t *testing.T) {
	root := t.TempDir()
	t.Setenv("JIG_CACHE_DIR", filepath.Join(root, "cache"))

	makeSource := func(name string, files map[string]string) string {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Join(dir, "skills"), 0o755); err != nil {
			t.Fatal(err)
		}
		for rel, content := range files {
			path := filepath.Join(dir, "skills", rel)
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
		return dir
	}
	ez := makeSource("ez-skills", map[string]string{
		"A/SKILL.md": "skill A\n", "B/SKILL.md": "skill B\n", "README.md": "ez readme\n",
	})
	awesome := makeSource("awesome-skills", map[string]string{
		"C/SKILL.md": "skill C\n", "D/SKILL.md": "skill D\n", "README.md": "awesome readme\n",
	})

	state := emptyState()
	model := Model{Entries: map[string]Entry{
		".agents/skills": {Path: ".agents/skills", Identity: "skills", Kind: EntryDir,
			Dir: &Dir{Src: SrcList{{Src: ez + "#skills"}, {Src: awesome + "#skills"}}}},
	}}
	ensure := func() string {
		var out bytes.Buffer
		if err := ensureDir(&out, root, &model, &state, ".agents/skills", true, false, newFileFetcher(), nil, nil); err != nil {
			t.Fatalf("ensureDir: %v", err)
		}
		return out.String()
	}

	got := ensure()
	if !strings.Contains(got, "5 added") || !strings.Contains(got, "1 shadowed") {
		t.Fatalf("first run = %q, want 5 added and 1 shadowed", got)
	}
	for _, skill := range []string{"A", "B", "C", "D"} {
		if !pathExists(filepath.Join(root, ".agents", "skills", skill, "SKILL.md")) {
			t.Fatalf("expected skill %s materialized", skill)
		}
	}
	// The first source wins the README conflict.
	if data, _ := os.ReadFile(filepath.Join(root, ".agents", "skills", "README.md")); string(data) != "ez readme\n" {
		t.Fatalf("README = %q, want ez readme", data)
	}
	if got := ensure(); !strings.Contains(got, "present-dir:") {
		t.Fatalf("second run = %q, want present-dir", got)
	}

	// An update in the second source flows through the merge.
	if err := os.WriteFile(filepath.Join(awesome, "skills", "C", "SKILL.md"), []byte("skill C v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, awesome, "commit", "-qam", "v2")
	if got := ensure(); !strings.Contains(got, "1 updated") {
		t.Fatalf("update run = %q, want 1 updated", got)
	}
	if data, _ := os.ReadFile(filepath.Join(root, ".agents", "skills", "C", "SKILL.md")); string(data) != "skill C v2\n" {
		t.Fatalf("C = %q, want v2", data)
	}
}

func TestDirSourcesGatedByOnlyWhen(t *testing.T) {
	root := t.TempDir()
	t.Setenv("JIG_CACHE_DIR", filepath.Join(root, "cache"))

	makeSource := func(name, file, content string) string {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Join(dir, "skills"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "skills", file), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		gitIn(t, dir, "init", "-q")
		gitIn(t, dir, "add", ".")
		gitIn(t, dir, "commit", "-qm", "init")
		return dir
	}
	base := makeSource("base-skills", "base.md", "base\n")
	billing := makeSource("billing-skills", "billing.md", "billing\n")

	state := emptyState()
	model := Model{Entries: map[string]Entry{
		"billing/api": {Path: "billing/api", Identity: "billing-api", Kind: EntryRepo,
			Repo: &Repo{Git: "git@example.com:billing.git"}},
		".agents/skills": {Path: ".agents/skills", Identity: "skills", Kind: EntryDir,
			Dir: &Dir{Src: SrcList{
				{Src: base + "#skills"},
				{Src: billing + "#skills", OnlyWhen: &Condition{Path: "billing"}},
			}}},
	}}
	ensure := func(activeRepos map[string]bool) string {
		var out bytes.Buffer
		if err := ensureDir(&out, root, &model, &state, ".agents/skills", true, false, newFileFetcher(), activeRepos, nil); err != nil {
			t.Fatalf("ensureDir: %v", err)
		}
		return out.String()
	}

	// Without billing active, only the base source materializes.
	ensure(nil)
	if !pathExists(filepath.Join(root, ".agents", "skills", "base.md")) {
		t.Fatal("expected base.md")
	}
	if pathExists(filepath.Join(root, ".agents", "skills", "billing.md")) {
		t.Fatal("did not expect billing.md without billing active")
	}

	// Activating billing brings the gated source in.
	ensure(map[string]bool{"billing/api": true})
	if !pathExists(filepath.Join(root, ".agents", "skills", "billing.md")) {
		t.Fatal("expected billing.md with billing active")
	}

	// Deactivating removes the gated source's untouched files.
	got := ensure(nil)
	if pathExists(filepath.Join(root, ".agents", "skills", "billing.md")) {
		t.Fatalf("expected billing.md removed after deactivation, output: %q", got)
	}
	if !strings.Contains(got, "1 deleted") {
		t.Fatalf("output = %q, want 1 deleted", got)
	}
	if !pathExists(filepath.Join(root, ".agents", "skills", "base.md")) {
		t.Fatal("expected base.md untouched")
	}
}

func TestDirLinksCreateSymlinksToTargetDir(t *testing.T) {
	root := t.TempDir()
	t.Setenv("JIG_CACHE_DIR", filepath.Join(root, "cache"))
	remote := filepath.Join(root, "remote")
	if err := os.MkdirAll(filepath.Join(remote, "skills", "A"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(remote, "skills", "A", "SKILL.md"), []byte("A\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, remote, "init", "-q")
	gitIn(t, remote, "add", ".")
	gitIn(t, remote, "commit", "-qm", "init")

	state := emptyState()
	model := Model{Entries: map[string]Entry{
		".agents/skills": {Path: ".agents/skills", Identity: "skills", Kind: EntryDir,
			Dir: &Dir{Src: SrcList{{Src: remote + "#skills"}}}},
		".opencode/skills": {Path: ".opencode/skills", Identity: "opencode-skills", Kind: EntryDir,
			Dir: &Dir{Link: ".agents/skills"}},
	}}
	fetcher := newFileFetcher()
	if err := ensureDir(ioDiscard{}, root, &model, &state, ".agents/skills", true, false, fetcher, nil, nil); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := ensureDir(&out, root, &model, &state, ".opencode/skills", true, false, fetcher, nil, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "linked-dir: .opencode/skills") {
		t.Fatalf("output = %q", out.String())
	}
	target, err := os.Readlink(filepath.Join(root, ".opencode", "skills"))
	if err != nil {
		t.Fatal(err)
	}
	if target != "../.agents/skills" {
		t.Fatalf("symlink target = %q", target)
	}
	// The skill is reachable through the link.
	if data, err := os.ReadFile(filepath.Join(root, ".opencode", "skills", "A", "SKILL.md")); err != nil || string(data) != "A\n" {
		t.Fatalf("through link: %q, %v", data, err)
	}
	// Second run is a no-op.
	out.Reset()
	if err := ensureDir(&out, root, &model, &state, ".opencode/skills", true, false, fetcher, nil, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "present-dir:") {
		t.Fatalf("second run = %q", out.String())
	}

	// Link dirs are active only when their target is active.
	active := activeDirsForRepoSet(&model, map[string]bool{"x": true}, nil, nil, false)
	if !active[".opencode/skills"] || !active[".agents/skills"] {
		t.Fatalf("active = %#v", active)
	}
	// Ordering puts the target before the link.
	ordered := orderDirsForApply(&model, active)
	if len(ordered) != 2 || ordered[0] != ".agents/skills" {
		t.Fatalf("ordered = %#v", ordered)
	}
}

func TestDirLinkValidation(t *testing.T) {
	def := testDefinition(t, `{
  "version": 1,
  "tree": {
    "a": { "$dir": { "id": "a", "link": "b" } },
    "b": { "$dir": { "id": "b", "link": "a" } },
    "c": { "$dir": { "id": "c", "src": "git@example.com:x.git#s", "link": "a" } },
    "d": { "$dir": { "id": "d", "link": "missing" } }
  }
}`)
	result := validateDefinition(def)
	joined := strings.Join(result.Errors, "\n")
	for _, want := range []string{"dir link cycle detected", "must define exactly one of src or link", "does not resolve to any dir"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("errors = %#v, missing %q", result.Errors, want)
		}
	}
}
