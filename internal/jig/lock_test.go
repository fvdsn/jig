package jig

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAcquireLockIsExclusive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock")
	unlock, err := acquireLock(path, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := acquireLock(path, 150*time.Millisecond); err == nil {
		t.Fatal("expected second acquisition to fail while held")
	} else if !strings.Contains(err.Error(), "another jig command") {
		t.Fatalf("unexpected error: %v", err)
	}
	unlock()
	unlock2, err := acquireLock(path, 150*time.Millisecond)
	if err != nil {
		t.Fatalf("expected acquisition after release: %v", err)
	}
	unlock2()
}

func TestAcquireLockWaitsForRelease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock")
	unlock, err := acquireLock(path, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(200 * time.Millisecond)
		unlock()
	}()
	unlock2, err := acquireLock(path, 2*time.Second)
	if err != nil {
		t.Fatalf("expected lock after holder released: %v", err)
	}
	unlock2()
}

func TestReleaseHeldLocksRemovesEveryHeldLock(t *testing.T) {
	dir := t.TempDir()
	workspace := filepath.Join(dir, "workspace.lock")
	mirror := filepath.Join(dir, "mirror.lock")
	unlock, err := acquireLock(workspace, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	unlockMirror, err := createLockFile(mirror)
	if err != nil {
		t.Fatal(err)
	}
	released, err := createLockFile(filepath.Join(dir, "released.lock"))
	if err != nil {
		t.Fatal(err)
	}
	released()

	releaseHeldLocks()

	for _, path := range []string{workspace, mirror} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("%s still exists after interrupt cleanup", path)
		}
	}
	// The deferred releases still run after the cleanup and must stay
	// harmless.
	unlock()
	unlockMirror()
	if _, err := acquireLock(workspace, 150*time.Millisecond); err != nil {
		t.Fatalf("expected acquisition after cleanup: %v", err)
	}
}

// deadPid returns the pid of a process that has already exited.
func deadPid(t *testing.T) int {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	return cmd.Process.Pid
}

func writeLock(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAcquireLockTakesOverDeadHolder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock")
	writeLock(t, path, fmt.Sprintf("%d\n", deadPid(t)))
	unlock, err := acquireLock(path, 150*time.Millisecond)
	if err != nil {
		t.Fatalf("expected a dead holder's lock to be taken over: %v", err)
	}
	defer unlock()
	if pid, ok := lockHolder(path); !ok || pid != os.Getpid() {
		t.Fatalf("lock records holder %d %v, want %d", pid, ok, os.Getpid())
	}
}

func TestAcquireLockKeepsLiveHolder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock")
	writeLock(t, path, fmt.Sprintf("%d\n", os.Getpid()))
	_, err := acquireLock(path, 150*time.Millisecond)
	if err == nil {
		t.Fatal("expected a live holder's lock to be kept")
	}
	if want := fmt.Sprintf("pid %d", os.Getpid()); !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q does not name the holder %q", err, want)
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("live holder's lock was removed: %v", statErr)
	}
}

func TestAcquireLockKeepsUnknownHolder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock")
	writeLock(t, path, "")
	_, err := acquireLock(path, 150*time.Millisecond)
	if err == nil {
		t.Fatal("expected a holderless lock to be kept")
	}
	if !strings.Contains(err.Error(), "unknown pid") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStealAgedLockOnlyWithoutHolder(t *testing.T) {
	dir := t.TempDir()
	old := time.Now().Add(-11 * time.Minute)

	live := filepath.Join(dir, "live.lock")
	writeLock(t, live, fmt.Sprintf("%d\n", os.Getpid()))
	if err := os.Chtimes(live, old, old); err != nil {
		t.Fatal(err)
	}
	if stealAgedLock(live) {
		t.Fatal("an aged lock with a live holder was stolen")
	}

	fresh := filepath.Join(dir, "fresh.lock")
	writeLock(t, fresh, "")
	if stealAgedLock(fresh) {
		t.Fatal("a fresh holderless lock was stolen")
	}

	aged := filepath.Join(dir, "aged.lock")
	writeLock(t, aged, "")
	if err := os.Chtimes(aged, old, old); err != nil {
		t.Fatal(err)
	}
	if !stealAgedLock(aged) {
		t.Fatal("an aged holderless lock was kept")
	}
	if _, err := os.Stat(aged); !os.IsNotExist(err) {
		t.Fatalf("stolen lock still exists: %v", err)
	}
}

func TestTryLockMirrorTakesOverDeadHolder(t *testing.T) {
	mirror := filepath.Join(t.TempDir(), "repo.git")
	writeLock(t, mirror+".lock", fmt.Sprintf("%d\n", deadPid(t)))
	release, ok := tryLockMirror(mirror)
	if !ok {
		t.Fatal("expected a dead holder's mirror lock to be taken over")
	}
	release()

	writeLock(t, mirror+".lock", fmt.Sprintf("%d\n", os.Getpid()))
	if _, ok := tryLockMirror(mirror); ok {
		t.Fatal("a live holder's mirror lock was taken over")
	}
}
