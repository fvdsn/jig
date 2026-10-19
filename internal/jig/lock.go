package jig

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// heldLocks tracks the lock files this process holds so an interrupt can
// remove them; the deferred releases never run when a signal kills the
// process. Creation and removal happen under the mutex so the handler
// cannot miss a lock created while it is cleaning up.
var (
	heldLocksMu sync.Mutex
	heldLocks   = map[string]struct{}{}
)

// createLockFile atomically creates the lock file, recording the holder's
// pid, and returns its release. os.ErrExist means another holder has it.
func createLockFile(path string) (func(), error) {
	heldLocksMu.Lock()
	defer heldLocksMu.Unlock()
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	_, _ = fmt.Fprintf(file, "%d\n", os.Getpid())
	_ = file.Close()
	heldLocks[path] = struct{}{}
	return func() {
		heldLocksMu.Lock()
		defer heldLocksMu.Unlock()
		delete(heldLocks, path)
		_ = os.Remove(path)
	}, nil
}

// lockHolder returns the pid recorded in the lock file; ok is false when
// the file is missing, empty, or malformed (a crash between creating the
// file and writing the pid, or a lock from an older jig).
func lockHolder(path string) (pid int, ok bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	pid, err = strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, true
}

// stealDeadLock removes the lock file when the process that recorded itself
// as the holder no longer exists (killed with SIGKILL, a closed terminal, a
// crash: anything the interrupt handler cannot catch), and reports whether
// it did. A lock whose holder is alive or unknown is left alone; a reused
// pid therefore keeps a stale lock in place, which is the safe direction.
func stealDeadLock(path string) bool {
	pid, ok := lockHolder(path)
	if !ok || processAlive(pid) {
		return false
	}
	// Claim by renaming first: only one waiter's rename can succeed, so two
	// waiters that both saw the dead holder cannot each remove the file and
	// have the second removal delete the first's fresh lock.
	stolen := fmt.Sprintf("%s.stale-%d", path, os.Getpid())
	if err := os.Rename(path, stolen); err != nil {
		return false
	}
	// The file may have been replaced between the read and the rename; a
	// live holder's fresh lock goes back where it was.
	if fresh, ok := lockHolder(stolen); ok && fresh != pid && processAlive(fresh) {
		if os.Rename(stolen, path) == nil {
			return false
		}
	}
	_ = os.Remove(stolen)
	return true
}

// releaseHeldLocks removes every lock file this process still holds.
func releaseHeldLocks() {
	heldLocksMu.Lock()
	defer heldLocksMu.Unlock()
	for path := range heldLocks {
		_ = os.Remove(path)
		delete(heldLocks, path)
	}
}

// ReleaseLocksOnInterrupt makes an interrupt, hangup, or termination signal
// remove the held lock files before the process exits, so a ctrl-c or a
// closed terminal does not leave the next jig command reporting a stale
// lock. The exit status is the conventional 128 plus the signal number.
func ReleaseLocksOnInterrupt() {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGHUP, syscall.SIGTERM)
	go func() {
		sig := <-signals
		releaseHeldLocks()
		status := 1
		if number, ok := sig.(syscall.Signal); ok {
			status = 128 + int(number)
		}
		os.Exit(status)
	}()
}

// acquireLock takes an exclusive lock file, waiting up to wait for a
// concurrent holder to release it. A lock left by a holder that no longer
// runs is taken over; one whose holder is alive is never stolen on age,
// since state mutations can legitimately run long (an initial clone of a
// large workspace), so it is reported with a removal hint instead.
func acquireLock(path string, wait time.Duration) (func(), error) {
	deadline := time.Now().Add(wait)
	for {
		release, err := createLockFile(path)
		if err == nil {
			return release, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if stealDeadLock(path) {
			continue
		}
		if time.Now().After(deadline) {
			holder := "unknown pid"
			if pid, ok := lockHolder(path); ok {
				holder = fmt.Sprintf("pid %d", pid)
			}
			return nil, fmt.Errorf("another jig command is running in this workspace (%s, locked by %s; delete the file if it is stale)", holder, path)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
