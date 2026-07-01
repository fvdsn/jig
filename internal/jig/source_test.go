package jig

import "testing"

func TestParseFileSrc(t *testing.T) {
	parsed, err := parseFileSrc("git:git@example.com:config.git#scripts/dev.sh")
	if err != nil {
		t.Fatal(err)
	}
	if parsed.GitURL != "git@example.com:config.git" || parsed.Path != "scripts/dev.sh" {
		t.Fatalf("unexpected parsed src: %#v", parsed)
	}
	if _, err := parseFileSrc("git:git@example.com:config.git#../bad"); err == nil {
		t.Fatal("expected invalid source path")
	}
}
