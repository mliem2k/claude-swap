//go:build !windows

package cswap

import "syscall"

// execClaude mirrors the POSIX branch of SessionManager._exec: replaces
// the current process image with claude. Never returns on success; the
// lock (if any) must already be released by the caller before this runs,
// since an exec'd claude must never inherit a held flock.
func execClaude(claudeBin string, argv []string, env []string) error {
	return syscall.Exec(claudeBin, argv, env)
}
