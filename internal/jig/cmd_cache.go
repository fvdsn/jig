package jig

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// CacheInfo prints the cache location, mirror count, and total size.
func CacheInfo(out io.Writer) error {
	root, enabled := cacheRoot()
	if !enabled {
		fmt.Fprintln(out, "cache disabled (JIG_CACHE_DIR is empty)")
		return nil
	}
	mirrors := listMirrors(root)
	var total int64
	for _, dir := range mirrors {
		total += dirSize(dir)
	}
	fmt.Fprintf(out, "dir: %s\n", root)
	fmt.Fprintf(out, "mirrors: %d\n", len(mirrors))
	fmt.Fprintf(out, "size: %s\n", humanSize(total))
	return nil
}

type CacheCleanOptions struct {
	UnusedDays int // remove mirrors unused for at least this many days; <0 removes all
}

// CacheClean removes cached mirrors. Mirrors locked by another process are
// skipped. Removing a mirror never affects existing workspace clones.
func CacheClean(options CacheCleanOptions, out io.Writer) error {
	root, enabled := cacheRoot()
	if !enabled {
		fmt.Fprintln(out, "cache disabled (JIG_CACHE_DIR is empty)")
		return nil
	}
	cutoff := time.Now().AddDate(0, 0, -options.UnusedDays)
	var removed int
	var freed int64
	for _, dir := range listMirrors(root) {
		lastUsed := mirrorLastUsed(dir)
		if options.UnusedDays >= 0 && lastUsed.After(cutoff) {
			continue
		}
		unlock, ok := tryLockMirror(dir)
		if !ok {
			fmt.Fprintf(out, "skipped (in use): %s\n", filepath.Base(dir))
			continue
		}
		size := dirSize(dir)
		err := os.RemoveAll(dir)
		unlock()
		if err != nil {
			fmt.Fprintf(out, "skipped: %s: %s\n", filepath.Base(dir), err)
			continue
		}
		removed++
		freed += size
		fmt.Fprintf(out, "removed: %s (%s, last used %s)\n", filepath.Base(dir), humanSize(size), humanAge(lastUsed))
	}
	fmt.Fprintf(out, "removed %d mirrors, freed %s\n", removed, humanSize(freed))
	return nil
}

func listMirrors(root string) []string {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var mirrors []string
	for _, entry := range entries {
		if entry.IsDir() && strings.HasSuffix(entry.Name(), ".git") {
			dir := filepath.Join(root, entry.Name())
			if pathExists(filepath.Join(dir, "HEAD")) {
				mirrors = append(mirrors, dir)
			}
		}
	}
	sort.Strings(mirrors)
	return mirrors
}

func dirSize(dir string) int64 {
	var total int64
	_ = filepath.WalkDir(dir, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if info, err := entry.Info(); err == nil && entry.Type().IsRegular() {
			total += info.Size()
		}
		return nil
	})
	return total
}

func humanSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	value := float64(bytes)
	for _, suffix := range []string{"KiB", "MiB", "GiB", "TiB"} {
		value /= unit
		if value < unit {
			return fmt.Sprintf("%.1f %s", value, suffix)
		}
	}
	return fmt.Sprintf("%.1f PiB", value/unit)
}

func humanAge(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	days := int(time.Since(t).Hours() / 24)
	if days <= 0 {
		return "today"
	}
	if days == 1 {
		return "1 day ago"
	}
	return fmt.Sprintf("%d days ago", days)
}
