package jig

import "testing"

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
