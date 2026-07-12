//go:build !windows

package cswap

// enableWindowsVT mirrors _enable_windows_vt's non-Windows branch: always
// true, matching Python's "if sys.platform != 'win32': return True".
func enableWindowsVT() bool { return true }
