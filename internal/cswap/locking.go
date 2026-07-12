package cswap

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"
)

// FileLock mirrors locking.FileLock: a cross-process exclusive lock. POSIX
// uses fcntl flock semantics; Windows uses LockFileEx, both via gofrs/flock.
type FileLock struct {
	lockPath string
	timeout  time.Duration
	fl       *flock.Flock
}

// NewFileLock mirrors FileLock.__init__.
func NewFileLock(lockPath string, timeout time.Duration) *FileLock {
	return &FileLock{lockPath: lockPath, timeout: timeout}
}

// Acquire mirrors acquire(timeout). Returns true on success, false on timeout.
// Creates the parent directory first (matching the Python mkdir(parents=True)).
func (l *FileLock) Acquire(timeout time.Duration) bool {
	if err := os.MkdirAll(filepath.Dir(l.lockPath), 0o700); err != nil {
		return false
	}
	l.fl = flock.New(l.lockPath)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	// retryDelay mirrors the Python loop's 0.1s sleep between attempts.
	ok, err := l.fl.TryLockContext(ctx, 100*time.Millisecond)
	if err != nil || !ok {
		return false
	}
	return true
}

// Release mirrors release. Safe to call when not held.
func (l *FileLock) Release() {
	if l.fl != nil {
		_ = l.fl.Unlock()
		l.fl = nil
	}
}

// Lock is the context-manager-equivalent helper: Acquire with the given
// timeout, returning an error wrapping ErrLock on failure so callers can
// defer Release.
func (l *FileLock) Lock(timeout time.Duration) error {
	if l.Acquire(timeout) {
		return nil
	}
	return fmt.Errorf("failed to acquire lock %s: %w", l.lockPath, ErrLock)
}
