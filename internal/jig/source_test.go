package jig

import (
	"path/filepath"
	"testing"
)

func TestParseFileSrc(t *testing.T) {
	// Plain form and the legacy git: prefix parse identically.
	for _, src := range []string{
		"git@example.com:config.git#scripts/dev.sh",
		"git:git@example.com:config.git#scripts/dev.sh",
	} {
		parsed, err := parseFileSrc(src)
		if err != nil {
			t.Fatalf("%s: %v", src, err)
		}
		if parsed.GitURL != "git@example.com:config.git" || parsed.Path != "scripts/dev.sh" {
			t.Fatalf("%s: unexpected parsed src: %#v", src, parsed)
		}
	}
	// The git:// protocol is a URL scheme, not the legacy prefix.
	parsed, err := parseFileSrc("git://example.com/config.git#dev.sh")
	if err != nil || parsed.GitURL != "git://example.com/config.git" {
		t.Fatalf("git protocol url: %#v, %v", parsed, err)
	}
	if _, err := parseFileSrc("git@example.com:config.git#../bad"); err == nil {
		t.Fatal("expected invalid source path")
	}
	if _, err := parseFileSrc("git@example.com:config.git"); err == nil {
		t.Fatal("expected missing file path error")
	}
}

func TestParseDirSrc(t *testing.T) {
	parsed, err := parseDirSrc("git@example.com:config.git#scripts")
	if err != nil || parsed.GitURL != "git@example.com:config.git" || parsed.Path != "scripts" {
		t.Fatalf("subtree: %#v, %v", parsed, err)
	}
	parsed, err = parseDirSrc("git@example.com:config.git")
	if err != nil || parsed.GitURL != "git@example.com:config.git" || parsed.Path != "" {
		t.Fatalf("whole repo: %#v, %v", parsed, err)
	}
	if _, err := parseDirSrc("git@example.com:config.git#"); err == nil {
		t.Fatal("expected empty subtree path error")
	}
}

func TestParseForgeFileSrc(t *testing.T) {
	cases := []struct{ src, gitURL, refPath string }{
		{"https://github.com/acme/config/blob/main/scripts/dev.sh",
			"https://github.com/acme/config.git", "main/scripts/dev.sh"},
		{"https://github.com/acme/config/blob/main/scripts/dev.sh#L10-L20",
			"https://github.com/acme/config.git", "main/scripts/dev.sh"},
		{"https://github.com/acme/config/raw/main/dev.sh",
			"https://github.com/acme/config.git", "main/dev.sh"},
		{"https://gitlab.com/acme/group/config/-/blob/main/dev.sh?ref_type=heads",
			"https://gitlab.com/acme/group/config.git", "main/dev.sh"},
		{"https://bitbucket.org/acme/config/src/main/dev.sh",
			"https://bitbucket.org/acme/config.git", "main/dev.sh"},
		{"https://codeberg.org/acme/config/src/branch/main/dev.sh",
			"https://codeberg.org/acme/config.git", "main/dev.sh"},
		{"https://raw.githubusercontent.com/acme/config/refs/heads/main/dev.sh",
			"https://github.com/acme/config.git", "main/dev.sh"},
		{"https://raw.githubusercontent.com/acme/config/main/dev.sh",
			"https://github.com/acme/config.git", "main/dev.sh"},
	}
	for _, c := range cases {
		parsed, err := parseFileSrc(c.src)
		if err != nil {
			t.Fatalf("%s: %v", c.src, err)
		}
		if parsed.GitURL != c.gitURL || parsed.RefPath != c.refPath || parsed.Path != "" {
			t.Fatalf("%s: unexpected parsed src: %#v", c.src, parsed)
		}
	}
	// A tree URL names a directory, not a file.
	if _, err := parseFileSrc("https://github.com/acme/config/tree/main/scripts"); err == nil {
		t.Fatal("expected directory URL error")
	}
	// An https clone URL with an explicit #path is not a forge web URL.
	parsed, err := parseFileSrc("https://github.com/acme/config.git#scripts/dev.sh")
	if err != nil || parsed.GitURL != "https://github.com/acme/config.git" ||
		parsed.Path != "scripts/dev.sh" || parsed.RefPath != "" {
		t.Fatalf("clone url: %#v, %v", parsed, err)
	}
}

func TestParseForgeDirSrc(t *testing.T) {
	parsed, err := parseDirSrc("https://github.com/acme/config/tree/main/skills")
	if err != nil || parsed.GitURL != "https://github.com/acme/config.git" || parsed.RefPath != "main/skills" {
		t.Fatalf("tree: %#v, %v", parsed, err)
	}
	// A tree URL at the repository root has only the ref as its tail.
	parsed, err = parseDirSrc("https://gitlab.com/acme/config/-/tree/main")
	if err != nil || parsed.GitURL != "https://gitlab.com/acme/config.git" || parsed.RefPath != "main" {
		t.Fatalf("root tree: %#v, %v", parsed, err)
	}
	if _, err := parseDirSrc("https://github.com/acme/config/blob/main/dev.sh"); err == nil {
		t.Fatal("expected file URL error")
	}
}

func TestResolveSrcPath(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	testRemoteRepo(t, repo)
	// A branch name containing a slash exercises the prefix split.
	if _, err := git(repo, "branch", "-m", "feature/x"); err != nil {
		t.Fatal(err)
	}

	path, err := resolveSrcPath(repo, fileSrc{RefPath: "feature/x/scripts/dev.sh"})
	if err != nil || path != "scripts/dev.sh" {
		t.Fatalf("path = %q, %v", path, err)
	}
	// The tail may be just the branch: the repository root.
	path, err = resolveSrcPath(repo, fileSrc{RefPath: "feature/x"})
	if err != nil || path != "" {
		t.Fatalf("root = %q, %v", path, err)
	}
	if _, err := resolveSrcPath(repo, fileSrc{RefPath: "other/dev.sh"}); err == nil {
		t.Fatal("expected non-default-branch error")
	}
	// Plain sources pass through without consulting the repository.
	path, err = resolveSrcPath(repo, fileSrc{Path: "scripts/dev.sh"})
	if err != nil || path != "scripts/dev.sh" {
		t.Fatalf("plain = %q, %v", path, err)
	}
	// A $file source must name a file inside the repository.
	if _, err := resolveSrcFilePath(repo, fileSrc{RefPath: "feature/x"}); err == nil {
		t.Fatal("expected missing file path error")
	}
}
