package cswap

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFileLockAcquireRelease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.lock")
	l := NewFileLock(path, 2*time.Second)

	if !l.Acquire(2 * time.Second) {
		t.Fatal("first acquire should succeed")
	}
	defer l.Release()

	// A second locker on the same path must time out (exclusive lock).
	other := NewFileLock(path, 200*time.Millisecond)
	if other.Acquire(200 * time.Millisecond) {
		t.Fatal("second exclusive acquire should time out while held")
	}
}

func TestFileLockLockReturnsErrLockOnTimeout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.lock")
	first := NewFileLock(path, 2*time.Second)
	if err := first.Lock(2 * time.Second); err != nil {
		t.Fatal(err)
	}
	defer first.Release()

	second := NewFileLock(path, 100*time.Millisecond)
	err := second.Lock(100 * time.Millisecond)
	if !errors.Is(err, ErrLock) {
		t.Fatalf("expected ErrLock, got %v", err)
	}
}

func TestFileLockCreatesParentDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "dir", "test.lock")
	l := NewFileLock(path, time.Second)
	if !l.Acquire(time.Second) {
		t.Fatal("acquire should succeed, creating parent dir")
	}
	defer l.Release()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("lock file should exist: %v", err)
	}
}
