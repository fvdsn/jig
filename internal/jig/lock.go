package jig

import (
	"errors"
	"fmt"
	"os"
	"time"
)

// createLockFile atomically creates the lock file, recording the holder's
// pid, and returns its release. os.ErrExist means another holder has it.
func createLockFile(path string) (func(), error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	_, _ = fmt.Fprintf(file, "%d\n", os.Getpid())
	_ = file.Close()
	return func() { _ = os.Remove(path) }, nil
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
