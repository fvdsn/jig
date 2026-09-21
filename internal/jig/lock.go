package jig

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
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

// releaseHeldLocks removes every lock file this process still holds.
func releaseHeldLocks() {
	heldLocksMu.Lock()
	defer heldLocksMu.Unlock()
	for path := range heldLocks {
		_ = os.Remove(path)
		delete(heldLocks, path)
	}
}

// ReleaseLocksOnInterrupt makes an interrupt or termination signal remove
// the held lock files before the process exits, so a ctrl-c does not leave
// the next jig command reporting a stale lock. The exit status is the
// conventional 128 plus the signal number.
func ReleaseLocksOnInterrupt() {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
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
// concurrent holder to release it. There is no stale-lock stealing: state
// mutations can legitimately run long (an initial clone of a large
// workspace), so a crashed process's lock is reported with a removal hint
// instead of being silently taken over.
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
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("another jig command is running in this workspace (locked by %s; delete the file if it is stale)", path)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
