//go:build !windows

package cswap

import "os"

// isRunningAsRoot mirrors main()'s `os.geteuid() == 0` guard. Windows has
// no analogous concept in this codebase; see cli_root_windows.go.
func isRunningAsRoot() bool {
	return os.Geteuid() == 0
}
