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
	resolveLinkPaths(&model)
	ensure := func() string {
		var out bytes.Buffer
		if err := ensureDir(&out, root, &model, &state, "tools/scripts", true, newFileFetcher(), nil, nil); err != nil {
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
  "version": 2,
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
	resolveLinkPaths(&model)
	ensure := func() string {
		var out bytes.Buffer
		if err := ensureDir(&out, root, &model, &state, ".agents/skills", true, newFileFetcher(), nil, nil); err != nil {
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
	// The last source wins the README conflict: sources layer base-first,
	// overrides after.
	if data, _ := os.ReadFile(filepath.Join(root, ".agents", "skills", "README.md")); string(data) != "awesome readme\n" {
		t.Fatalf("README = %q, want awesome readme", data)
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

func TestEnsureDirSkipsUnavailableSources(t *testing.T) {
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
	base := makeSource("base-skills", map[string]string{"A/SKILL.md": "skill A\n"})
	extra := makeSource("extra-skills", map[string]string{"B/SKILL.md": "skill B\n"})
	restoreExtra := func(content string) {
		if err := os.MkdirAll(filepath.Join(extra, "skills", "B"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(extra, "skills", "B", "SKILL.md"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		gitIn(t, extra, "add", ".")
		gitIn(t, extra, "commit", "-qm", "restore skills")
	}

	state := emptyState()
	model := Model{Entries: map[string]Entry{
		".agents/skills": {Path: ".agents/skills", Identity: "skills", Kind: EntryDir,
			Dir: &Dir{Src: SrcList{{Src: base + "#skills"}, {Src: extra + "#skills"}}}},
	}}
	resolveLinkPaths(&model)
	ensure := func() string {
		var out bytes.Buffer
		if err := ensureDir(&out, root, &model, &state, ".agents/skills", true, newFileFetcher(), nil, nil); err != nil {
			t.Fatalf("ensureDir: %v", err)
		}
		return out.String()
	}

	// Initial materialization with one source's subtree missing upstream
	// still writes the available sources and names the broken one.
	gitIn(t, extra, "rm", "-rq", "skills")
	gitIn(t, extra, "commit", "-qm", "drop skills")
	got := ensure()
	if !strings.Contains(got, "wrote-dir") || !strings.Contains(got, extra+"#skills unavailable") {
		t.Fatalf("first run = %q, want wrote-dir with unavailable note", got)
	}
	if !pathExists(filepath.Join(root, ".agents", "skills", "A", "SKILL.md")) {
		t.Fatal("expected available source materialized")
	}

	// The subtree appearing upstream flows into the merge.
	restoreExtra("skill B\n")
	if got := ensure(); !strings.Contains(got, "1 added") {
		t.Fatalf("recovery run = %q, want 1 added", got)
	}

	// While a source is unavailable its files are kept, not deleted.
	gitIn(t, extra, "rm", "-rq", "skills")
	gitIn(t, extra, "commit", "-qm", "drop skills again")
	if got := ensure(); strings.Contains(got, "deleted") || !strings.Contains(got, "unavailable") {
		t.Fatalf("unavailable run = %q, want kept files and unavailable note", got)
	}
	if !pathExists(filepath.Join(root, ".agents", "skills", "B", "SKILL.md")) {
		t.Fatal("expected unavailable source's files kept")
	}

	// The kept files stayed tracked: once the source resolves again, an
	// untouched file that changed upstream is overwritten, not left stale.
	restoreExtra("skill B v2\n")
	if got := ensure(); !strings.Contains(got, "1 updated") {
		t.Fatalf("restore run = %q, want 1 updated", got)
	}
	if data, _ := os.ReadFile(filepath.Join(root, ".agents", "skills", "B", "SKILL.md")); string(data) != "skill B v2\n" {
		t.Fatalf("B = %q, want v2", data)
	}
}

func TestEnsureDirErrorsWhenNoSourceResolves(t *testing.T) {
	root := t.TempDir()
	t.Setenv("JIG_CACHE_DIR", filepath.Join(root, "cache"))

	good := filepath.Join(root, "good-skills")
	if err := os.MkdirAll(filepath.Join(good, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(good, "skills", "SKILL.md"), []byte("skill\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, good, "init", "-q")
	gitIn(t, good, "add", ".")
	gitIn(t, good, "commit", "-qm", "init")

	badSrc := good + "#no-such-subtree"
	state := emptyState()
	model := Model{Entries: map[string]Entry{
		".agents/skills": {Path: ".agents/skills", Identity: "skills", Kind: EntryDir,
			Dir: &Dir{Src: SrcList{{Src: badSrc}}}},
	}}
	resolveLinkPaths(&model)

	var out bytes.Buffer
	err := ensureDir(&out, root, &model, &state, ".agents/skills", true, newFileFetcher(), nil, nil)
	if err == nil || !strings.Contains(err.Error(), badSrc) {
		t.Fatalf("err = %v, want mention of failing source %s", err, badSrc)
	}
	if pathExists(filepath.Join(root, ".agents", "skills")) {
		t.Fatalf("directory materialized despite no resolvable source")
	}
}

func TestEnsureDirKeepsForeignSymlinks(t *testing.T) {
	root := t.TempDir()
	t.Setenv("JIG_CACHE_DIR", filepath.Join(root, "cache"))

	source := filepath.Join(root, "skills-src")
	skillFile := filepath.Join(source, "skills", "jig", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skillFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skillFile, []byte("the skill\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A second file keeps the skills tree alive when SKILL.md is dropped.
	if err := os.WriteFile(filepath.Join(source, "skills", "README.md"), []byte("readme\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, source, "init", "-q")
	gitIn(t, source, "add", ".")
	gitIn(t, source, "commit", "-qm", "init")

	state := emptyState()
	model := Model{Entries: map[string]Entry{
		".agents/skills": {Path: ".agents/skills", Identity: "skills", Kind: EntryDir,
			Dir: &Dir{Src: SrcList{{Src: source + "#skills"}}}},
	}}
	resolveLinkPaths(&model)
	ensure := func() string {
		var out bytes.Buffer
		if err := ensureDir(&out, root, &model, &state, ".agents/skills", true, newFileFetcher(), nil, nil); err != nil {
			t.Fatalf("ensureDir: %v", err)
		}
		return out.String()
	}

	if got := ensure(); !strings.Contains(got, "2 added") {
		t.Fatalf("first run = %q, want 2 added", got)
	}

	// Replace the materialized file with a self-looping symlink, like a
	// leftover from an older schema where the link direction was reversed.
	target := filepath.Join(root, ".agents", "skills", "jig", "SKILL.md")
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("SKILL.md", target); err != nil {
		t.Fatal(err)
	}

	if got := ensure(); !strings.Contains(got, "1 modified kept") {
		t.Fatalf("symlink run = %q, want 1 modified kept", got)
	}
	if !isSymlink(target) {
		t.Fatal("expected the foreign symlink to be left in place")
	}

	// When the file also vanishes upstream, the symlink is abandoned, not
	// deleted, and later runs report a clean directory.
	if err := os.Remove(skillFile); err != nil {
		t.Fatal(err)
	}
	gitIn(t, source, "commit", "-qam", "drop skill")
	if got := ensure(); !strings.Contains(got, "1 left untracked") {
		t.Fatalf("vanish run = %q, want 1 left untracked", got)
	}
	if !isSymlink(target) {
		t.Fatal("expected the abandoned symlink to be left in place")
	}
	if got := ensure(); !strings.Contains(got, "present-dir:") {
		t.Fatalf("final run = %q, want present-dir", got)
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
				{Src: billing + "#skills", OnlyWhen: &Condition{Ref: Ref{Path: "billing/*"}}},
			}}},
	}}
	resolveLinkPaths(&model)
	ensure := func(activeRepos map[string]bool) string {
		var out bytes.Buffer
		if err := ensureDir(&out, root, &model, &state, ".agents/skills", true, newFileFetcher(), activeRepos, nil); err != nil {
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
			Dir: &Dir{Link: &Ref{Path: ".agents/skills"}}},
	}}
	resolveLinkPaths(&model)
	fetcher := newFileFetcher()
	if err := ensureDir(ioDiscard{}, root, &model, &state, ".agents/skills", true, fetcher, nil, nil); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := ensureDir(&out, root, &model, &state, ".opencode/skills", true, fetcher, nil, nil); err != nil {
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
	if err := ensureDir(&out, root, &model, &state, ".opencode/skills", true, fetcher, nil, nil); err != nil {
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
  "version": 2,
  "tree": {
    "a": { "$dir": { "id": "a", "link": {"path": "b"} } },
    "b": { "$dir": { "id": "b", "link": {"path": "a"} } },
    "c": { "$dir": { "id": "c", "src": "git@example.com:x.git#s", "link": {"path": "a"} } },
    "d": { "$dir": { "id": "d", "link": {"path": "missing"} } }
  }
}`)
	result := validateDefinition(def)
	joined := strings.Join(result.Errors, "\n")
	for _, want := range []string{"dir link cycle detected", "must define exactly one of src, link, or copy", "does not resolve to any dir"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("errors = %#v, missing %q", result.Errors, want)
		}
	}
}

func TestDirCopyMaterializesTargetSources(t *testing.T) {
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
		".claude/skills": {Path: ".claude/skills", Identity: "claude-skills", Kind: EntryDir,
			Dir: &Dir{Copy: &Ref{Path: ".agents/skills"}}},
	}}
	resolveLinkPaths(&model)
	fetcher := newFileFetcher()

	// The copy materializes a real directory without the target installed.
	var out bytes.Buffer
	if err := ensureDir(&out, root, &model, &state, ".claude/skills", true, fetcher, nil, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "wrote-dir: .claude/skills (1 added)") {
		t.Fatalf("output = %q", out.String())
	}
	copyPath := filepath.Join(root, ".claude", "skills")
	if isSymlink(copyPath) {
		t.Fatal("copy is a symlink")
	}
	if data, err := os.ReadFile(filepath.Join(copyPath, "A", "SKILL.md")); err != nil || string(data) != "A\n" {
		t.Fatalf("copied file: %q, %v", data, err)
	}

	// An upstream change reaches the copy on the next run.
	if err := os.WriteFile(filepath.Join(remote, "skills", "A", "SKILL.md"), []byte("A v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, remote, "add", "-A")
	gitIn(t, remote, "commit", "-qm", "v2")
	out.Reset()
	if err := ensureDir(&out, root, &model, &state, ".claude/skills", true, newFileFetcher(), nil, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "updated-dir: .claude/skills (1 updated)") {
		t.Fatalf("update run = %q", out.String())
	}

	// Copies follow the same activation and ordering rules as links.
	active := activeDirsForRepoSet(&model, map[string]bool{"x": true}, nil, nil, false)
	if !active[".claude/skills"] || !active[".agents/skills"] {
		t.Fatalf("active = %#v", active)
	}
	ordered := orderDirsForApply(&model, active)
	if len(ordered) != 2 || ordered[0] != ".agents/skills" {
		t.Fatalf("ordered = %#v", ordered)
	}
}

func TestDirCopyLinkTransitions(t *testing.T) {
	root := t.TempDir()
	t.Setenv("JIG_CACHE_DIR", filepath.Join(root, "cache"))
	remote := filepath.Join(root, "remote")
	if err := os.MkdirAll(filepath.Join(remote, "skills", "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(remote, "skills", "sub", "SKILL.md"), []byte("S\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, remote, "init", "-q")
	gitIn(t, remote, "add", ".")
	gitIn(t, remote, "commit", "-qm", "init")

	entries := func(alias *Dir) Model {
		return Model{Entries: map[string]Entry{
			".agents/skills": {Path: ".agents/skills", Identity: "skills", Kind: EntryDir,
				Dir: &Dir{Src: SrcList{{Src: remote + "#skills"}}}},
			".claude/skills": {Path: ".claude/skills", Identity: "claude-skills", Kind: EntryDir, Dir: alias},
		}}
	}
	linkModel := entries(&Dir{Link: &Ref{Path: ".agents/skills"}})
	copyModel := entries(&Dir{Copy: &Ref{Path: ".agents/skills"}})
	resolveLinkPaths(&linkModel)
	resolveLinkPaths(&copyModel)
	state := emptyState()
	fetcher := newFileFetcher()
	aliasAbs := filepath.Join(root, ".claude", "skills")

	// Materialize as a link first.
	if err := ensureDir(ioDiscard{}, root, &linkModel, &state, ".agents/skills", true, fetcher, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := ensureDir(ioDiscard{}, root, &linkModel, &state, ".claude/skills", true, fetcher, nil, nil); err != nil {
		t.Fatal(err)
	}
	if !isSymlink(aliasAbs) {
		t.Fatal("expected symlink after link run")
	}

	// link -> copy: the jig-owned symlink is replaced by a real directory.
	var out bytes.Buffer
	if err := ensureDir(&out, root, &copyModel, &state, ".claude/skills", true, fetcher, nil, nil); err != nil {
		t.Fatal(err)
	}
	if isSymlink(aliasAbs) {
		t.Fatal("still a symlink after copy run")
	}
	if !strings.Contains(out.String(), "wrote-dir: .claude/skills") {
		t.Fatalf("copy run = %q", out.String())
	}

	// copy -> link with a local modification is refused.
	subFile := filepath.Join(aliasAbs, "sub", "SKILL.md")
	if err := os.WriteFile(subFile, []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureDir(ioDiscard{}, root, &linkModel, &state, ".claude/skills", true, fetcher, nil, nil); err == nil ||
		!strings.Contains(err.Error(), "locally modified") {
		t.Fatalf("modified copy -> link err = %v", err)
	}

	// copy -> link with a clean manifest replaces the files with the symlink.
	if err := os.WriteFile(subFile, []byte("S\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := ensureDir(&out, root, &linkModel, &state, ".claude/skills", true, fetcher, nil, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "linked-dir: .claude/skills") {
		t.Fatalf("link run = %q", out.String())
	}
	if !isSymlink(aliasAbs) {
		t.Fatal("expected symlink after converting back")
	}
}

func TestDirCopyValidation(t *testing.T) {
	def := testDefinition(t, `{
  "version": 2,
  "tree": {
    "a": { "$dir": { "id": "a", "src": "git@example.com:x.git#s" } },
    "b": { "$dir": { "id": "b", "link": {"path": "a"} } },
    "c": { "$dir": { "id": "c", "copy": {"path": "b"} } },
    "d": { "$dir": { "id": "d", "src": "git@example.com:x.git#s", "copy": {"path": "a"} } },
    "e": { "$dir": { "id": "e", "copy": {"path": "e"} } }
  }
}`)
	result := validateDefinition(def)
	joined := strings.Join(result.Errors, "\n")
	for _, want := range []string{
		"copy target b must define src",
		"dir d must define exactly one of src, link, or copy",
		"dir e cannot copy to itself",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("errors = %#v, missing %q", result.Errors, want)
		}
	}
}

func TestEnsureDirLocalSources(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("JIG_CACHE_DIR", filepath.Join(root, "cache"))

	gitSource := filepath.Join(root, "skills-src")
	if err := os.MkdirAll(filepath.Join(gitSource, "skills", "A"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitSource, "skills", "A", "SKILL.md"), []byte("A\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, gitSource, "init", "-q")
	gitIn(t, gitSource, "add", ".")
	gitIn(t, gitSource, "commit", "-qm", "init")

	state := emptyState()
	model := Model{Entries: map[string]Entry{
		".agents/skills": {Path: ".agents/skills", Identity: "skills", Kind: EntryDir,
			Dir: &Dir{Src: SrcList{
				{Src: gitSource + "#skills"},
				{Dir: "~/.codabox/skills", Optional: true},
			}}},
	}}
	resolveLinkPaths(&model)
	ensure := func() string {
		var out bytes.Buffer
		if err := ensureDir(&out, root, &model, &state, ".agents/skills", true, newFileFetcher(), nil, nil); err != nil {
			t.Fatalf("ensureDir: %v", err)
		}
		return out.String()
	}

	// An absent optional local dir is silently gated off.
	if got := ensure(); !strings.Contains(got, "wrote-dir") || strings.Contains(got, "unavailable") {
		t.Fatalf("first run = %q, want quiet wrote-dir", got)
	}

	// The local dir appearing merges in; last listed source wins conflicts.
	localSkills := filepath.Join(home, ".codabox", "skills")
	if err := os.MkdirAll(filepath.Join(localSkills, "B"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(localSkills, "B", "SKILL.md"), []byte("B\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(localSkills, "A", "SKILL.md"), []byte("A local\n"), 0o644); err != nil {
		if err := os.MkdirAll(filepath.Join(localSkills, "A"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(localSkills, "A", "SKILL.md"), []byte("A local\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got := ensure()
	if !strings.Contains(got, "added") || !strings.Contains(got, "shadowed") {
		t.Fatalf("appear run = %q, want additions with a shadowed conflict", got)
	}
	if data, _ := os.ReadFile(filepath.Join(root, ".agents", "skills", "A", "SKILL.md")); string(data) != "A local\n" {
		t.Fatalf("A = %q, want the local override to win", data)
	}
	if !pathExists(filepath.Join(root, ".agents", "skills", "B", "SKILL.md")) {
		t.Fatal("expected local skill merged")
	}

	// Local edits flow through on the next sync.
	if err := os.WriteFile(filepath.Join(localSkills, "B", "SKILL.md"), []byte("B v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ensure(); !strings.Contains(got, "1 updated") {
		t.Fatalf("edit run = %q, want 1 updated", got)
	}

	// Removing the optional dir converges: its untouched files are deleted.
	if err := os.RemoveAll(localSkills); err != nil {
		t.Fatal(err)
	}
	if got := ensure(); !strings.Contains(got, "deleted") {
		t.Fatalf("remove run = %q, want deletions", got)
	}
	if pathExists(filepath.Join(root, ".agents", "skills", "B")) {
		t.Fatal("expected local skill removed after the source vanished")
	}
	if data, _ := os.ReadFile(filepath.Join(root, ".agents", "skills", "A", "SKILL.md")); string(data) != "A\n" {
		t.Fatalf("A = %q, want git content restored", data)
	}
}
