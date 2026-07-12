package cswap

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSetupLoggingCreatesDirLazily(t *testing.T) {
	root := t.TempDir()
	logDir := filepath.Join(root, "deep", "logs")

	// Constructing the logger must NOT create the dir yet.
	logger, _ := SetupLogging(logDir, false)
	if _, err := os.Stat(logDir); !os.IsNotExist(err) {
		t.Fatalf("log dir should not exist yet, got err=%v", err)
	}

	// First write creates it.
	logger.Info("hello", "key", "val")
	if _, err := os.Stat(filepath.Join(logDir, "claude-swap.log")); err != nil {
		t.Fatalf("log file should exist after a record: %v", err)
	}
}

func TestSetupLoggingReturnsCloseableLogFile(t *testing.T) {
	dir := t.TempDir()
	_, logFile := SetupLogging(dir, false)
	if logFile == nil {
		t.Fatal("expected a non-nil *lumberjack.Logger")
	}
	if err := logFile.Close(); err != nil {
		t.Fatalf("expected Close to succeed, got %v", err)
	}
}
