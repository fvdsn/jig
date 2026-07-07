package jig

import (
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
