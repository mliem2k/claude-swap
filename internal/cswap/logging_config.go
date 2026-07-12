package cswap

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"gopkg.in/natefinch/lumberjack.v2"
)

// lazyMkdirWriter wraps a writer and creates the log file's parent directory
// on the first Write, mirroring _LazyDirRotatingFileHandler. The lumberjack
// rotator itself owns size-based rotation (1MB, 3 backups).
type lazyMkdirWriter struct {
	once   sync.Once
	dir    string
	target *lumberjack.Logger
}

func (w *lazyMkdirWriter) Write(p []byte) (int, error) {
	var firstErr error
	w.once.Do(func() {
		if err := os.MkdirAll(w.dir, 0o700); err != nil {
			firstErr = err
		}
	})
	if firstErr != nil {
		return 0, firstErr
	}
	return w.target.Write(p)
}

// SetupLogging mirrors setup_logging. Also returns the underlying
// lumberjack.Logger so callers that need to release the file handle before
// deleting logDir (Purge, on Windows: an open log file blocks
// os.RemoveAll) can call its Close method first, mirroring Python's
// explicit handler.close() loop in purge().
func SetupLogging(logDir string, debug bool) (*slog.Logger, *lumberjack.Logger) {
	logFile := filepath.Join(logDir, "claude-swap.log")
	base := &lumberjack.Logger{
		Filename:   logFile,
		MaxSize:    1, // 1MB
		MaxBackups: 3,
	}
	fileWriter := &lazyMkdirWriter{dir: logDir, target: base}

	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}

	var w io.Writer = fileWriter
	if debug {
		w = io.MultiWriter(fileWriter, os.Stderr)
	}
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: level})), base
}
