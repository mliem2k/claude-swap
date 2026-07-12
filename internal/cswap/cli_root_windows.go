//go:build windows

package cswap

// isRunningAsRoot mirrors main()'s root guard's POSIX-only nature: Python
// only calls os.geteuid() when sys.platform != "win32" is implied by the
// attribute existing at all (geteuid is POSIX-only; Windows has no
// equivalent concept in this codebase), so this is always false here.
func isRunningAsRoot() bool { return false }
