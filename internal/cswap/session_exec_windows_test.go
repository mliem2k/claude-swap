//go:build windows

package cswap

import (
	"os"
	"os/exec"
	"runtime"
	"testing"
)

func TestExecClaudeMirrorsExitCodeWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-only")
	}
	if os.Getenv("CSWAP_TEST_EXEC_CHILD") == "1" {
		_ = execClaude("cmd.exe", []string{"cmd.exe", "/c", "exit", "7"}, os.Environ())
		return // unreachable if execClaude behaves correctly: os.Exit fires first
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestExecClaudeMirrorsExitCodeWindows$")
	cmd.Env = append(os.Environ(), "CSWAP_TEST_EXEC_CHILD=1")
	err := cmd.Run()
	exitErr, ok := err.(*exec.ExitError)
	if !ok || exitErr.ExitCode() != 7 {
		t.Fatalf("got err=%v", err)
	}
}
