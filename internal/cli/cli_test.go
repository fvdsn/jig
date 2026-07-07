package cli

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestParseInitArgsCloneWithoutPath(t *testing.T) {
	parsed, err := parseArgs([]string{"git@example.com:config.git", "--clone", "--with-optional-deps", "--archived"}, initFlags)
	if err != nil {
		t.Fatal(err)
	}
	if !parsed.Flags["--clone"] {
		t.Fatal("expected clone flag")
	}
	if parsed.Values["--clone"] != "" {
		t.Fatalf("expected empty clone path, got %q", parsed.Values["--clone"])
	}
	if !parsed.Flags["--with-optional-deps"] {
		t.Fatal("expected optional deps flag")
	}
	if !parsed.Flags["--archived"] {
		t.Fatal("expected archived flag")
	}
}

func TestParseInitArgsCloneWithPath(t *testing.T) {
	parsed, err := parseArgs([]string{"git@example.com:config.git", "--clone", "services/checkout"}, initFlags)
	if err != nil {
		t.Fatal(err)
	}
	if !parsed.Flags["--clone"] || parsed.Values["--clone"] != "services/checkout" {
		t.Fatalf("unexpected clone parse: %#v", parsed)
	}
}

func TestInitSelectionFlagsRequireClone(t *testing.T) {
	err := cmdInit([]string{"git@example.com:config.git", "--archived"}, io.Discard)
	if err == nil || err.Error() != "--no-deps, --with-optional-deps, --archived, and --tags require --clone" {
		t.Fatalf("expected --clone requirement, got %v", err)
	}
}

func TestParseUpdateFlags(t *testing.T) {
	parsed, err := parseArgs([]string{"--sync", "--with-optional-deps", "--archived"}, map[string]flagKind{"--sync": boolFlag, "--with-optional-deps": boolFlag, "--archived": boolFlag})
	if err != nil {
		t.Fatal(err)
	}
	if !parsed.Flags["--sync"] || !parsed.Flags["--with-optional-deps"] || !parsed.Flags["--archived"] {
		t.Fatalf("unexpected update flags: %#v", parsed.Flags)
	}
}

func TestUpdateSelectionFlagsRequireSync(t *testing.T) {
	err := cmdUpdate([]string{"--archived"}, io.Discard)
	if err == nil || err.Error() != "--no-deps, --with-optional-deps, --archived, and --prune require --sync" {
		t.Fatalf("expected --sync requirement, got %v", err)
	}
}

func TestPerCommandHelp(t *testing.T) {
	var out bytes.Buffer
	if err := Run([]string{"clone", "--help"}, &out, io.Discard); err != nil {
		t.Fatalf("clone --help: %v", err)
	}
	if !strings.Contains(out.String(), "usage: jig clone [path]") {
		t.Fatalf("clone --help output:\n%s", out.String())
	}

	out.Reset()
	if err := Run([]string{"help", "rm"}, &out, io.Discard); err != nil {
		t.Fatalf("help rm: %v", err)
	}
	if !strings.Contains(out.String(), "usage: jig rm <path>...") {
		t.Fatalf("help rm output:\n%s", out.String())
	}

	if err := Run([]string{"help", "nonsense"}, &out, io.Discard); err == nil {
		t.Fatal("expected error for unknown help topic")
	}

	// Usage errors derive from the same table.
	err := Run([]string{"clone", "a", "b"}, io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "usage: jig clone [path]") {
		t.Fatalf("usage error = %v", err)
	}
}
