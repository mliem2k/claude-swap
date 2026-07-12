//go:build windows

package cswap

import (
	"os"

	"golang.org/x/sys/windows"
)

// enableWindowsVT mirrors _enable_windows_vt: enables ANSI/VT processing on
// the Windows console. Returns false (color unsupported) if the console
// mode can't be queried or set, e.g. output is redirected to a file/pipe
// rather than a real console.
func enableWindowsVT() bool {
	const enableVirtualTerminalProcessing = 0x0004
	handle := windows.Handle(os.Stdout.Fd())
	var mode uint32
	if err := windows.GetConsoleMode(handle, &mode); err != nil {
		return false
	}
	if err := windows.SetConsoleMode(handle, mode|enableVirtualTerminalProcessing); err != nil {
		return false
	}
	return true
}
