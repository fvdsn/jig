package jig

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// The repo cache keeps one bare mirror per remote URL. Clones go
// mirror-first: the mirror is created or freshened with a cheap fetch, then
// the workspace clone is made locally from it, which hardlinks immutable
// object files (fast, near-free on disk) and leaves the checkout fully
// independent of the cache. Every cache failure falls back to a direct
// network clone, so the cache can never make an operation fail.
//
// JIG_CACHE_DIR overrides the location; setting it to an empty string
// disables the cache.

// cacheRoot returns the mirror directory and whether the cache is enabled.
func cacheRoot() (string, bool) {
	if value, ok := os.LookupEnv("JIG_CACHE_DIR"); ok {
		if value == "" {
			return "", false
		}
		return value, true
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return "", false
	}
	return filepath.Join(base, "jig", "repos"), true
}

var mirrorNameSanitizer = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func mirrorDir(root string, gitURL string) string {
	sum := sha256.Sum256([]byte(gitURL))
	name := mirrorNameSanitizer.ReplaceAllString(strings.TrimSuffix(gitURL, ".git"), "-")
	name = strings.Trim(name, "-")
	if len(name) > 80 {
		name = name[len(name)-80:]
	}
	return filepath.Join(root, name+"-"+hex.EncodeToString(sum[:6])+".git")
}

// lockMirror serializes mirror creation and updates across processes. Locks
// older than ten minutes are considered abandoned and stolen.
func lockMirror(dir string) (func(), error) {
	lockPath := dir + ".lock"
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(2 * time.Minute)
	for {
		file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			fmt.Fprintf(file, "%d\n", os.Getpid())
			file.Close()
			return func() { os.Remove(lockPath) }, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if info, statErr := os.Stat(lockPath); statErr == nil && time.Since(info.ModTime()) > 10*time.Minute {
			os.Remove(lockPath)
			continue
		}
		if time.Now().After(deadline) {
			return nil, errors.New("timed out waiting for cache lock")
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// lastUsedMarker records when a mirror was last successfully used, so cache
// eviction has exact data. Git ignores unknown files in a bare repository.
const lastUsedMarker = "jig-last-used"

func touchLastUsed(dir string) {
	path := filepath.Join(dir, lastUsedMarker)
	now := time.Now()
	if err := os.Chtimes(path, now, now); err != nil {
		_ = os.WriteFile(path, nil, 0o644)
	}
}

// mirrorLastUsed reports when the mirror was last used; zero when unknown.
func mirrorLastUsed(dir string) time.Time {
	if info, err := os.Stat(filepath.Join(dir, lastUsedMarker)); err == nil {
		return info.ModTime()
	}
	return time.Time{}
}

// tryLockMirror acquires the mirror lock without waiting; ok is false when
// another process holds it.
func tryLockMirror(dir string) (func(), bool) {
	lockPath := dir + ".lock"
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, false
	}
	fmt.Fprintf(file, "%d\n", os.Getpid())
	file.Close()
	return func() { os.Remove(lockPath) }, true
}

// ensureMirror creates or freshens the bare mirror for gitURL and returns
// its directory. The caller must hold the mirror lock.
func ensureMirror(root string, gitURL string) (string, error) {
	dir := mirrorDir(root, gitURL)
	if pathExists(filepath.Join(dir, "HEAD")) {
		if _, err := git(dir, "remote", "update", "--prune"); err != nil {
			return "", err
		}
		touchLastUsed(dir)
		return dir, nil
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}
	if _, err := git("", "clone", "--mirror", gitURL, dir); err != nil {
		os.RemoveAll(dir)
		return "", err
	}
	touchLastUsed(dir)
	return dir, nil
}

// cloneRepo clones gitURL to targetAbs, going through the cache when
// possible and falling back to a direct network clone otherwise.
func cloneRepo(gitURL string, targetAbs string) error {
	if err := cloneViaCache(gitURL, targetAbs); err == nil {
		return nil
	}
	os.RemoveAll(targetAbs)
	_, err := git("", "clone", gitURL, targetAbs)
	return err
}

func cloneViaCache(gitURL string, targetAbs string) error {
	root, enabled := cacheRoot()
	if !enabled {
		return errors.New("cache disabled")
	}
	unlock, err := lockMirror(mirrorDir(root, gitURL))
	if err != nil {
		return err
	}
	defer unlock()
	dir, err := ensureMirror(root, gitURL)
	if err != nil {
		return err
	}
	if _, err := git("", "clone", dir, targetAbs); err != nil {
		os.RemoveAll(targetAbs)
		return err
	}
	if _, err := git(targetAbs, "remote", "set-url", "origin", gitURL); err != nil {
		os.RemoveAll(targetAbs)
		return err
	}
	return nil
}

// cacheShowFile reads ref:path from the freshened mirror of gitURL without
// materializing a checkout. ref defaults to the mirror's HEAD.
func cacheShowFile(gitURL string, ref string, sourcePath string) ([]byte, error) {
	root, enabled := cacheRoot()
	if !enabled {
		return nil, errors.New("cache disabled")
	}
	unlock, err := lockMirror(mirrorDir(root, gitURL))
	if err != nil {
		return nil, err
	}
	defer unlock()
	dir, err := ensureMirror(root, gitURL)
	if err != nil {
		return nil, err
	}
	if ref == "" {
		ref = "HEAD"
	}
	out, err := git(dir, "show", ref+":"+sourcePath)
	if err != nil {
		return nil, err
	}
	return []byte(out), nil
}
