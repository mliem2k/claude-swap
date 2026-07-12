package cswap

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWithProperLockfileAcquiresAndReleases(t *testing.T) {
	dir := t.TempDir()
	lockDir := filepath.Join(dir, "target.lock")

	ran := false
	err := WithProperLockfile(context.Background(), lockDir, LockOpts{},
		func() error { ran = true; return nil })
	if err != nil {
		t.Fatal(err)
	}
	if !ran {
		t.Fatal("body did not run")
	}
	if _, statErr := os.Stat(lockDir); !os.IsNotExist(statErr) {
		t.Fatalf("lock dir should be removed on release, got err=%v", statErr)
	}
}

func TestWithProperLockfileBlocksSecondHolder(t *testing.T) {
	dir := t.TempDir()
	lockDir := filepath.Join(dir, "target.lock")
	acquired := make(chan struct{})
	release := make(chan struct{})

	go func() {
		_ = WithProperLockfile(context.Background(), lockDir, LockOpts{},
			func() error {
				close(acquired)
				<-release
				return nil
			})
	}()
	<-acquired

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	err := WithProperLockfile(ctx, lockDir, LockOpts{Timeout: 400 * time.Millisecond},
		func() error { return nil })
	close(release)
	if !errors.Is(err, ErrClaudeCodeLockTimeout) {
		t.Fatalf("expected ErrClaudeCodeLockTimeout, got %v", err)
	}
}

func TestWithProperLockfileTakesOverStaleLock(t *testing.T) {
	dir := t.TempDir()
	lockDir := filepath.Join(dir, "target.lock")
	mustMkdir(t, lockDir)
	oldTime := time.Now().Add(-30 * time.Second)
	if err := os.Chtimes(lockDir, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	ran := false
	err := WithProperLockfile(context.Background(), lockDir, LockOpts{Staleness: 10 * time.Second},
		func() error { ran = true; return nil })
	if err != nil {
		t.Fatalf("should take over stale lock, got %v", err)
	}
	if !ran {
		t.Fatal("body should run after takeover")
	}
}
