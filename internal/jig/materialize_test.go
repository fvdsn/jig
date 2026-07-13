package jig

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestActiveSourceURLsHonorsPerSourceConditions(t *testing.T) {
	model := Model{Entries: map[string]Entry{
		"billing/api": testRepoEntry("billing/api", "billing-api", Repo{Git: "git@example.com:billing.git"}),
		"AGENTS.md": testFileEntry("AGENTS.md", "agents", File{Src: SrcList{
			{Src: "git@example.com:config.git#agents/base.md"},
			{Src: "git@example.com:billing-docs.git#agents/billing.md", OnlyWhen: &Condition{Path: "billing"}},
		}}),
		"bin/dev": testFileEntry("bin/dev", "dev-command", File{Link: "AGENTS.md"}),
		".agents/skills": {Path: ".agents/skills", Identity: "skills", Kind: EntryDir,
			Dir: &Dir{Src: SrcList{
				{Src: "git@example.com:skills.git#skills"},
				{Src: "git@example.com:billing-skills.git#skills", OnlyWhen: &Condition{Path: "billing"}},
			}}},
	}}
	p := plan{
		Files: []string{"AGENTS.md", "bin/dev"},
		Dirs:  []string{".agents/skills"},
	}

	// Without billing active, gated sources stay out; the shared config and
	// skills repos are collected once each and links contribute nothing.
	got := activeSourceURLs(&model, p, nil, nil)
	want := []string{"git@example.com:config.git", "git@example.com:skills.git"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("urls = %#v, want %#v", got, want)
	}

	got = activeSourceURLs(&model, p, map[string]bool{"billing/api": true}, nil)
	want = []string{
		"git@example.com:billing-docs.git",
		"git@example.com:billing-skills.git",
		"git@example.com:config.git",
		"git@example.com:skills.git",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("urls with billing = %#v, want %#v", got, want)
	}
}

func TestPrefetchMirrorsServesLaterFetches(t *testing.T) {
	root := t.TempDir()
	t.Setenv("JIG_CACHE_DIR", filepath.Join(root, "cache"))
	source := filepath.Join(root, "config")
	testFileSource(t, source, map[string]string{"agents/base.md": "base\n"})

	fetcher := newFileFetcher()
	fetcher.prefetchMirrors([]string{source, filepath.Join(root, "missing")})

	// The good mirror is memoized and serves content; the bad one memoized
	// its error, which surfaces through the normal per-entry path.
	if _, ok := fetcher.mirrors[source]; !ok {
		t.Fatal("expected prefetched mirror to be memoized")
	}
	content, _, err := fetcher.content(source + "#agents/base.md")
	if err != nil || string(content) != "base\n" {
		t.Fatalf("content = %q, %v; want base", content, err)
	}
	if cached, ok := fetcher.mirrors[filepath.Join(root, "missing")]; !ok || cached.err == nil {
		t.Fatal("expected the failing mirror's error to be memoized")
	}

	state := emptyState()
	model := Model{Entries: map[string]Entry{
		"AGENTS.md": testFileEntry("AGENTS.md", "agents", File{Src: SrcList{{Src: source + "#agents/base.md"}}}),
	}}
	var out strings.Builder
	if err := ensureFile(&out, root, &model, &state, "AGENTS.md", true, fetcher, nil, nil); err != nil {
		t.Fatalf("ensureFile: %v", err)
	}
	if !strings.Contains(out.String(), "wrote-file:") {
		t.Fatalf("output = %q, want wrote-file", out.String())
	}
	if data, _ := os.ReadFile(filepath.Join(root, "AGENTS.md")); string(data) != "base\n" {
		t.Fatalf("content = %q, want base", data)
	}
}
