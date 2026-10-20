package jig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setTestHome points the home directory at dir on every platform.
func setTestHome(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
}

// Detection reads the marker directories a harness leaves in the home
// directory, in the known order, and never the workspace.
func TestDetectHarnessesReadsHomeMarkers(t *testing.T) {
	home := t.TempDir()
	setTestHome(t, home)
	if got := detectHarnesses(); len(got) != 0 {
		t.Fatalf("empty home detected %v", got)
	}
	for _, marker := range []string{".config/opencode", ".claude"} {
		if err := os.MkdirAll(filepath.Join(home, filepath.FromSlash(marker)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// A file where the marker directory would be is not a harness.
	if err := os.WriteFile(filepath.Join(home, ".codex"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := detectHarnesses(); strings.Join(got, ",") != "claude,opencode" {
		t.Fatalf("detected %v, want claude,opencode", got)
	}
}

func TestParseHarnesses(t *testing.T) {
	got, err := ParseHarnesses("codex, claude")
	if err != nil || strings.Join(got, ",") != "codex,claude" {
		t.Fatalf("list = %v, %v", got, err)
	}
	got, err = ParseHarnesses("none")
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("none = %#v, %v", got, err)
	}
	for _, bad := range []string{"", "vim", "claude,claude", "claude,none"} {
		if _, err := ParseHarnesses(bad); err == nil {
			t.Fatalf("%q was accepted", bad)
		}
	}
}

// The config field distinguishes absent (detect) from empty (none), and an
// unknown name is refused when the config loads.
func TestConfigHarnesses(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	setTestHome(t, home)
	if err := os.MkdirAll(filepath.Join(home, ".gemini"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := saveConfig(root, Config{Version: 1, Schema: "jig.json"}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(root, configFile))
	if strings.Contains(string(data), "harnesses") {
		t.Fatalf("absent list was written: %s", data)
	}
	config, err := loadConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	if harnesses, source := effectiveHarnesses(config); source != "detected" || strings.Join(harnesses, ",") != "gemini" {
		t.Fatalf("absent list: %v %s", harnesses, source)
	}

	none := []string{}
	if err := saveConfig(root, Config{Version: 1, Schema: "jig.json", Harnesses: &none}); err != nil {
		t.Fatal(err)
	}
	config, err = loadConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	if harnesses, source := effectiveHarnesses(config); source != "configured" || len(harnesses) != 0 {
		t.Fatalf("empty list: %v %s", harnesses, source)
	}

	if err := os.WriteFile(filepath.Join(root, configFile), []byte(`{"version": 1, "schema": "jig.json", "harnesses": ["claude", "emacs"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig(root); err == nil || !strings.Contains(err.Error(), `unknown harness "emacs"`) {
		t.Fatalf("unknown harness guard = %v", err)
	}
}
