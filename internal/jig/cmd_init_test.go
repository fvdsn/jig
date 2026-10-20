package jig

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitFromLocalFileCreatesSourceCheckout(t *testing.T) {
	root := t.TempDir()
	sourcePath := filepath.Join(root, "source.jig.json")
	workspacePath := filepath.Join(root, "workspace")
	if err := os.WriteFile(sourcePath, []byte(`{
  "version": 3,
  "tree": {
    "platform/auth": {
      "$repo": { "id": "auth-service", "git": "git@example.com:auth.git" }
    }
  }
}`), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := Init(InitOptions{
		SourceArg:    sourcePath,
		WorkspaceDir: workspacePath,
	}, &out); err != nil {
		t.Fatal(err)
	}

	config, err := loadConfig(workspacePath)
	if err != nil {
		t.Fatal(err)
	}
	if config.Schema != "jig.json" {
		t.Fatalf("expected schema jig.json, got %q", config.Schema)
	}
	def, err := loadDefinition(filepath.Join(workspacePath, sourceDir, "jig.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := flattenDefinition(def, nil); err != nil {
		t.Fatal(err)
	}
	if !isGitRepo(filepath.Join(workspacePath, sourceDir)) {
		t.Fatal("expected source checkout to be a git repository")
	}
	state, err := loadState(workspacePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Repos) != 0 || len(state.Files) != 0 {
		t.Fatalf("expected empty state, got %#v", state)
	}
}

func TestInitBareCreatesStarterWorkspace(t *testing.T) {
	useEmbeddedSkill(t)
	workspacePath := filepath.Join(t.TempDir(), "workspace")

	// The starter needs no network: the only content is the embedded jig
	// skill, so the clone step the CLI turns on for a bare init runs here.
	var out bytes.Buffer
	if err := Init(InitOptions{WorkspaceDir: workspacePath, Clone: true}, &out); err != nil {
		t.Fatal(err)
	}

	config, err := loadConfig(workspacePath)
	if err != nil {
		t.Fatal(err)
	}
	// The starter is YAML, the format for a schema written by hand.
	if config.Schema != "jig.yaml" {
		t.Fatalf("expected schema jig.yaml, got %q", config.Schema)
	}
	if !isGitRepo(filepath.Join(workspacePath, sourceDir)) {
		t.Fatal("expected source checkout to be a git repository")
	}
	def, err := loadDefinition(filepath.Join(workspacePath, sourceDir, "jig.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	model, err := flattenDefinition(def, nil)
	if err != nil {
		t.Fatal(err)
	}
	// The starter declares nothing: the skills directory comes from the
	// embedded jig skill.
	if _, ok := model.entry(".agents/skills", EntryDir); !ok {
		t.Fatal("expected the jig skill to derive .agents/skills")
	}
	if !pathExists(filepath.Join(workspacePath, ".agents", "skills", "jig", "SKILL.md")) {
		t.Fatalf("jig skill not installed:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "next steps:") || !strings.Contains(out.String(), "$EDITOR .jig/source/jig.yaml") {
		t.Fatalf("expected next-steps guidance, got:\n%s", out.String())
	}
}

func TestInitTreatsUnstatableSourceAsGitURL(t *testing.T) {
	// On Windows, statting an scp-style URL (git@host:path) fails with
	// ERROR_INVALID_NAME rather than ErrNotExist, so any stat failure must
	// fall through to git clone instead of surfacing the stat error. A path
	// whose parent is a regular file reproduces a non-ErrNotExist stat error
	// on every platform.
	root := t.TempDir()
	blocker := filepath.Join(root, "blocker")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	err := Init(InitOptions{
		SourceArg:    filepath.Join(blocker, "meta.git"),
		WorkspaceDir: filepath.Join(root, "workspace"),
	}, ioDiscard{})
	if err == nil {
		t.Fatal("expected the clone of a nonexistent repository to fail")
	}
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) {
		t.Fatalf("expected a git clone error, got a stat error: %v", err)
	}
}

func TestInitBareRejectsPathFlag(t *testing.T) {
	err := Init(InitOptions{
		WorkspaceDir: filepath.Join(t.TempDir(), "workspace"),
		SchemaPath:   "nested/jig.json",
	}, ioDiscard{})
	if err == nil || err.Error() != "--path can only be used with Git sources" {
		t.Fatalf("expected --path error, got %v", err)
	}
}

func TestInitFromLocalFileRejectsPathFlag(t *testing.T) {
	root := t.TempDir()
	sourcePath := filepath.Join(root, "source.jig.json")
	if err := os.WriteFile(sourcePath, []byte(`{"version":1,"tree":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	err := Init(InitOptions{
		SourceArg:    sourcePath,
		WorkspaceDir: filepath.Join(root, "workspace"),
		SchemaPath:   "nested/jig.json",
	}, ioDiscard{})
	if err == nil || err.Error() != "--path can only be used with Git sources" {
		t.Fatalf("expected --path error, got %v", err)
	}
}

// Init records the harness list when given and leaves detection on
// otherwise, and says which on its one harness line.
func TestInitRecordsHarnesses(t *testing.T) {
	useEmbeddedSkill(t)
	home := t.TempDir()
	setTestHome(t, home)
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}

	detected := filepath.Join(t.TempDir(), "detected")
	var out bytes.Buffer
	if err := Init(InitOptions{WorkspaceDir: detected}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "harnesses: codex (detected)\n") {
		t.Fatalf("detected init output:\n%s", out.String())
	}
	config, err := loadConfig(detected)
	if err != nil {
		t.Fatal(err)
	}
	if config.Harnesses != nil {
		t.Fatalf("detection must not be recorded: %v", *config.Harnesses)
	}

	chosen := filepath.Join(t.TempDir(), "chosen")
	harnesses := []string{"claude", "gemini"}
	out.Reset()
	if err := Init(InitOptions{WorkspaceDir: chosen, Harnesses: &harnesses}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "harnesses: claude, gemini (--harness)\n") {
		t.Fatalf("chosen init output:\n%s", out.String())
	}
	config, err = loadConfig(chosen)
	if err != nil {
		t.Fatal(err)
	}
	if config.Harnesses == nil || strings.Join(*config.Harnesses, ",") != "claude,gemini" {
		t.Fatalf("recorded harnesses = %v", config.Harnesses)
	}
	// The embedded jig skill derives the claude alias for the recorded
	// list.
	ws, err := loadWorkspaceAt(chosen, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ws.Model.entry(".claude/skills", EntryDir); !ok || ws.HarnessSource != "configured" {
		t.Fatalf("entries = %v (%s)", sortedEntryPaths(&ws.Model), ws.HarnessSource)
	}

	none := []string{}
	out.Reset()
	if err := Init(InitOptions{WorkspaceDir: filepath.Join(t.TempDir(), "none"), Harnesses: &none}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "harnesses: none (--harness)\n") {
		t.Fatalf("none init output:\n%s", out.String())
	}
}
