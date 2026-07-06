// Package integration drives jig end-to-end through the CLI entry point
// against real Git repositories, encoding the cross-command invariants that
// unit tests cannot see: init/clone/sync/rm lifecycles, schema evolution via
// update, and multi-command workflows.
package integration

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fvdsn/jig/internal/cli"
)

// world is one isolated integration scenario: a temp root holding source
// repositories, a schema repository, workspaces, and a private clone cache.
type world struct {
	t    *testing.T
	root string
}

func newWorld(t *testing.T) *world {
	t.Helper()
	root := t.TempDir()
	t.Setenv("JIG_CACHE_DIR", filepath.Join(root, "cache"))
	t.Setenv("GIT_AUTHOR_NAME", "it")
	t.Setenv("GIT_AUTHOR_EMAIL", "it@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "it")
	t.Setenv("GIT_COMMITTER_EMAIL", "it@example.com")
	return &world{t: t, root: root}
}

func (w *world) path(rel ...string) string {
	return filepath.Join(append([]string{w.root}, rel...)...)
}

// git runs a git command and fails the test on error.
func (w *world) git(dir string, args ...string) string {
	w.t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		w.t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
	return string(out)
}

// newRemote creates a working Git repository that acts as a remote: clones
// pull from it, and upstream changes are made by committing directly in it.
func (w *world) newRemote(name string, files map[string]string) string {
	w.t.Helper()
	dir := w.path("remotes", name)
	w.writeFiles(dir, files)
	w.git("", "init", "-q", dir)
	w.commitRemote(dir, nil, "init")
	return dir
}

// commitRemote writes files into a remote and commits everything.
func (w *world) commitRemote(dir string, files map[string]string, message string) {
	w.t.Helper()
	w.writeFiles(dir, files)
	w.git(dir, "add", "-A")
	w.git(dir, "commit", "-qm", message)
}

func (w *world) writeFiles(dir string, files map[string]string) {
	w.t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		w.t.Fatal(err)
	}
	for rel, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			w.t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			w.t.Fatal(err)
		}
	}
}

// jig runs a jig command with the working directory set to dir, returning
// its combined output. The command line and output land in the test log.
func (w *world) jig(dir string, args ...string) (string, error) {
	w.t.Helper()
	oldWd, err := os.Getwd()
	if err != nil {
		w.t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		w.t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(oldWd); err != nil {
			w.t.Fatal(err)
		}
	}()
	var out bytes.Buffer
	runErr := cli.Run(args, &out, &out)
	w.t.Logf("$ jig %s\n%s", strings.Join(args, " "), out.String())
	return out.String(), runErr
}

// mustJig runs a jig command and fails the test on error.
func (w *world) mustJig(dir string, args ...string) string {
	w.t.Helper()
	out, err := w.jig(dir, args...)
	if err != nil {
		w.t.Fatalf("jig %v failed: %v\n%s", args, err, out)
	}
	return out
}

func (w *world) exists(rel ...string) bool {
	_, err := os.Lstat(w.path(rel...))
	return err == nil
}

func (w *world) read(rel ...string) string {
	w.t.Helper()
	data, err := os.ReadFile(w.path(rel...))
	if err != nil {
		w.t.Fatalf("read %v: %v", rel, err)
	}
	return string(data)
}

func (w *world) assertContains(output string, wants ...string) {
	w.t.Helper()
	for _, want := range wants {
		if !strings.Contains(output, want) {
			w.t.Fatalf("output missing %q:\n%s", want, output)
		}
	}
}

func (w *world) assertNotContains(output string, rejects ...string) {
	w.t.Helper()
	for _, reject := range rejects {
		if strings.Contains(output, reject) {
			w.t.Fatalf("output unexpectedly contains %q:\n%s", reject, output)
		}
	}
}
