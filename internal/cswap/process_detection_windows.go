//go:build windows

package cswap

import "golang.org/x/sys/windows"

// isPIDAlivePlatform mirrors _is_pid_alive_windows: OpenProcess with
// PROCESS_QUERY_LIMITED_INFORMATION.
func isPIDAlivePlatform(pid int) bool {
	const processQueryLimitedInformation = 0x1000
	handle, err := windows.OpenProcess(processQueryLimitedInformation, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(handle)
	return true
}
