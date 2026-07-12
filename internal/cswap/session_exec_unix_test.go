//go:build !windows

package cswap

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

func TestExecClaudeReplacesProcessPOSIX(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX-only")
	}
	if os.Getenv("CSWAP_TEST_EXEC_CHILD") == "1" {
		err := execClaude("/bin/echo", []string{"/bin/echo", "hello-from-exec"}, os.Environ())
		// A successful execClaude never returns; reaching here is a failure.
		t.Fatalf("execClaude returned instead of replacing the process: %v", err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestExecClaudeReplacesProcessPOSIX$", "-test.v")
	cmd.Env = append(os.Environ(), "CSWAP_TEST_EXEC_CHILD=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("child process failed: %v\noutput: %s", err, out)
	}
	if !strings.Contains(string(out), "hello-from-exec") {
		t.Fatalf("expected child output to contain the exec'd program's output, got: %s", out)
	}
}

func TestExecClaudeNonexistentBinaryReturnsError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX-only")
	}
	err := execClaude("/nonexistent/binary/path", []string{"/nonexistent/binary/path"}, os.Environ())
	if err == nil {
		t.Fatal("expected an error for a nonexistent binary")
	}
}
