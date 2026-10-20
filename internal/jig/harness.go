package jig

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// knownHarnesses are the coding-agent harnesses the agents block can
// materialize for, in the order they are listed and reported.
var knownHarnesses = []string{"claude", "codex", "copilot", "cursor", "opencode", "gemini", "windsurf", "kiro"}

// harnessMarkers are the directories a harness leaves in the user's home
// once it has run. Detection reads them rather than the workspace, where
// jig itself creates .claude/ and the like: a root-level signal would detect
// a harness forever after the first sync.
var harnessMarkers = map[string]string{
	"claude":   ".claude",
	"codex":    ".codex",
	"copilot":  ".copilot",
	"cursor":   ".cursor",
	"opencode": ".config/opencode",
	"gemini":   ".gemini",
	"windsurf": ".codeium/windsurf",
	"kiro":     ".kiro",
}

func isKnownHarness(name string) bool {
	for _, known := range knownHarnesses {
		if known == name {
			return true
		}
	}
	return false
}

// validateHarnesses checks a configured harness list: known names only,
// each at most once.
func validateHarnesses(names []string) error {
	seen := map[string]bool{}
	for _, name := range names {
		if !isKnownHarness(name) {
			return fmt.Errorf("unknown harness %q; known harnesses are %s", name, strings.Join(knownHarnesses, ", "))
		}
		if seen[name] {
			return fmt.Errorf("harness %q is listed twice", name)
		}
		seen[name] = true
	}
	return nil
}

// ParseHarnesses parses the value of --harness: a comma-separated list of
// known harness names, or "none" for an empty list.
func ParseHarnesses(value string) ([]string, error) {
	names := []string{}
	for _, name := range strings.Split(value, ",") {
		if name = strings.TrimSpace(name); name != "" {
			names = append(names, name)
		}
	}
	if len(names) == 1 && names[0] == "none" {
		return []string{}, nil
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("--harness needs a comma-separated list of harnesses (%s) or none", strings.Join(knownHarnesses, ", "))
	}
	if err := validateHarnesses(names); err != nil {
		return nil, err
	}
	return names, nil
}

// detectHarnesses lists the known harnesses whose marker directory exists
// under the user's home, in the known order. Without a home directory
// nothing is detected.
func detectHarnesses() []string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return []string{}
	}
	detected := []string{}
	for _, name := range knownHarnesses {
		if info, err := os.Stat(filepath.Join(home, filepath.FromSlash(harnessMarkers[name]))); err == nil && info.IsDir() {
			detected = append(detected, name)
		}
	}
	return detected
}

// effectiveHarnesses resolves the harness list a workspace materializes
// for: the configured list when the config has one (including an empty
// one), otherwise the harnesses detected on this machine. The second value
// says where the list came from, for messages.
func effectiveHarnesses(config Config) ([]string, string) {
	if config.Harnesses != nil {
		return append([]string{}, (*config.Harnesses)...), "configured"
	}
	return detectHarnesses(), "detected"
}

// describeHarnesses renders a harness list for messages: "claude, codex"
// or "none".
func describeHarnesses(names []string) string {
	if len(names) == 0 {
		return "none"
	}
	return strings.Join(names, ", ")
}

// sortHarnesses orders a harness list in the known order, for stable
// derivation and output.
func sortHarnesses(names []string) []string {
	rank := map[string]int{}
	for i, name := range knownHarnesses {
		rank[name] = i
	}
	sorted := append([]string{}, names...)
	sort.SliceStable(sorted, func(i, j int) bool { return rank[sorted[i]] < rank[sorted[j]] })
	return sorted
}
