package cswap

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Constants mirroring claude_locks.py. proper-lockfile defaults claude-code
// runs with: stale after 10s, holder touches every stale/2 = 5s. We touch a
// little faster for margin.
const (
	stalenessDefault   = 10 * time.Second
	touchInterval      = 3 * time.Second
	defaultLockTimeout = 9 * time.Second
	lockPollInterval   = 250 * time.Millisecond
	lockPollJitterMax  = 250 * time.Millisecond
)

// CredentialsLockDir mirrors credentials_lock_dir (~/.claude.lock).
func CredentialsLockDir() string {
	home := GetClaudeConfigHome()
	return filepath.Join(filepath.Dir(home), filepath.Base(home)+".lock")
}

// ConfigLockDir mirrors config_lock_dir (~/.claude.json.lock).
func ConfigLockDir() string {
	path := GetGlobalConfigPath()
	return filepath.Join(filepath.Dir(path), filepath.Base(path)+".lock")
}

// LockOpts configures WithProperLockfile. Zero value uses the defaults.
type LockOpts struct {
	Timeout   time.Duration // default 9s
	Staleness time.Duration // default 10s
}

// WithProperLockfile mirrors the proper_lockfile context manager: acquires a
// directory lock via mkdir atomicity, takes over locks whose mtime is older
// than staleness, touches the mtime while held via a background goroutine, and
// removes the directory on exit. Returns an error wrapping
// ErrClaudeCodeLockTimeout if the lock stays held past the timeout.
func WithProperLockfile(ctx context.Context, lockDir string, opts LockOpts, fn func() error) error {
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = defaultLockTimeout
	}
	staleness := opts.Staleness
	if staleness == 0 {
		staleness = stalenessDefault
	}
	if err := os.MkdirAll(filepath.Dir(lockDir), 0o700); err != nil {
		return err
	}

	deadline := time.Now().Add(timeout)
	for {
		err := os.Mkdir(lockDir, 0o700)
		if err == nil {
			break
		}
		if !os.IsExist(err) {
			return err
		}
		if time.Now().After(deadline) {
			return newLockTimeout(lockDir)
		}
		info, statErr := os.Stat(lockDir)
		if statErr != nil {
			continue // holder released between mkdir and stat; retry now
		}
		if time.Since(info.ModTime()) > staleness {
			// Dead holder per the protocol: remove and retake. Losing the
			// rmdir/mkdir race to another waiter just means looping again.
			if rerr := os.Remove(lockDir); rerr != nil {
				time.Sleep(50 * time.Millisecond)
			}
			continue
		}
		// Clamp the poll wait to the remaining time until deadline, so the
		// deadline check at the top of the loop always gets a chance to run
		// before any longer external ctx timeout could fire in its place.
		wait := lockPollInterval + time.Duration(rand.Int63n(int64(lockPollJitterMax)))
		if remaining := time.Until(deadline); remaining < wait {
			wait = remaining
		}
		if wait < 0 {
			wait = 0
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Go(func() {
		ticker := time.NewTicker(touchInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				if err := os.Chtimes(lockDir, time.Now(), time.Now()); err != nil {
					return // lock stolen/removed; nothing left to keep alive
				}
			}
		}
	})

	runErr := fn()
	close(stop)
	wg.Wait()
	// Mirrors proper_lockfile's finally block: both failure shapes are
	// logged (matching Python's two _logger.warning calls), never
	// returned, so a best-effort cleanup failure never masks the body's
	// own error.
	if rerr := os.Remove(lockDir); rerr != nil {
		if os.IsNotExist(rerr) {
			slog.Default().Warn(fmt.Sprintf("lock %s vanished while held (taken over as stale?)", lockDir))
		} else {
			slog.Default().Warn(fmt.Sprintf("failed to release lock %s: %v", lockDir, rerr))
		}
	}
	return runErr
}

// WithCredentialsLock mirrors claude_credentials_lock.
func WithCredentialsLock(ctx context.Context, timeout time.Duration, fn func() error) error {
	return WithProperLockfile(ctx, CredentialsLockDir(), LockOpts{Timeout: timeout}, fn)
}

// WithConfigLock mirrors claude_config_lock.
func WithConfigLock(ctx context.Context, timeout time.Duration, fn func() error) error {
	return WithProperLockfile(ctx, ConfigLockDir(), LockOpts{Timeout: timeout}, fn)
}

// newLockTimeout mirrors the ClaudeCodeLockTimeout raise.
func newLockTimeout(lockDir string) error {
	return fmt.Errorf("could not acquire %s: %w", filepath.Base(lockDir), ErrClaudeCodeLockTimeout)
}
