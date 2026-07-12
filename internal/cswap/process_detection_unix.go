//go:build !windows

package cswap

import (
	"errors"
	"os"
	"syscall"
)

// isPIDAlivePlatform mirrors the POSIX branch of is_pid_alive: os.kill(pid, 0).
func isPIDAlivePlatform(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	if err == nil {
		return true
	}
	if errors.Is(err, os.ErrProcessDone) {
		return false
	}
	// EPERM means the process exists but we lack permission.
	return errors.Is(err, syscall.EPERM)
}
