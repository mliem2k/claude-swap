//go:build windows

package cswap

import (
	"os"
	"os/exec"
)

// execClaude mirrors the Windows branch of SessionManager._exec: os.exec*
// detaches from the console confusingly on Windows, so this stays resident
// as a thin wrapper and mirrors claude's exit code via os.Exit. Never
// returns.
func execClaude(claudeBin string, argv []string, env []string) error {
	cmd := exec.Command(claudeBin, argv[1:]...)
	cmd.Env = env
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	runErr := cmd.Run()
	rc := 0
	if exitErr, ok := runErr.(*exec.ExitError); ok {
		rc = exitErr.ExitCode()
	} else if runErr != nil {
		rc = 1
	}
	os.Exit(rc)
	return nil // unreachable
}
