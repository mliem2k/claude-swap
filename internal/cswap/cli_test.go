package cswap

import (
	"bytes"
	"fmt"
	"runtime"
	"strings"
	"testing"
)

func TestRunCLIVersionFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := RunCLI([]string{"cswap", "--version"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("got exit code %d, stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), version) {
		t.Fatalf("expected version string in output, got %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), runtime.Version()) {
		t.Fatalf("expected the Go runtime version in output, got %q", stdout.String())
	}
}

func TestRunCLIUnknownCommandExitsTwo(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := RunCLI([]string{"cswap", "not-a-real-command"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("got exit code %d, want 2 (argparse-style usage error), stderr=%q", code, stderr.String())
	}
}

func TestRunCLIMissingRequiredArgExitsTwo(t *testing.T) {
	newTestSwitcher(t)

	var stdout, stderr bytes.Buffer
	code := RunCLI([]string{"cswap", "remove"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("got exit code %d, want 2 (argparse-style usage error), stderr=%q", code, stderr.String())
	}
}

func TestRunCLISwitchJSONErrorWritesEnvelopeToStdout(t *testing.T) {
	newTestSwitcher(t)

	var stdout, stderr bytes.Buffer
	code := RunCLI([]string{"cswap", "switch", "--json"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("got exit code %d, want 1, stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), `"schemaVersion"`) {
		t.Fatalf("expected a JSON error envelope on stdout, got stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected stderr empty in JSON mode (stdout carries the error), got %q", stderr.String())
	}
}

// TestRunCLINoArgsNonInteractiveIsUsageError mirrors main()'s "no command
// given" parser.error(): a bare, non-interactive invocation (as every
// RunCLI test call is, since stdin/stdout are bytes.Buffer, never a real
// TTY) is a usage error (exit 2, stderr), not a quiet success. The
// interactive-TTY case (bare `cswap` opens the TUI) is exercised
// separately wherever isInteractiveTerminal itself is tested, since a
// unit test has no real terminal to open one against.
func TestRunCLINoArgsNonInteractiveIsUsageError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := RunCLI([]string{"cswap"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("got exit code %d, want 2 (usage error); stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "no command given") {
		t.Fatalf("expected the usage error message on stderr, got %q", stderr.String())
	}
}

// TestRunCLIHelpFlagStillPrintsHelp confirms --help itself is untouched
// by the "no command given" usage-error change: cobra's own built-in help
// handling short-circuits before the root command's RunE ever runs.
func TestRunCLIHelpFlagStillPrintsHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := RunCLI([]string{"cswap", "--help"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("got exit code %d, stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Usage") && !strings.Contains(stdout.String(), "cswap") {
		t.Fatalf("expected help text, got %q", stdout.String())
	}
}

func TestDispatchErrorWrapsClaudeSwitchErrorAsHandled(t *testing.T) {
	var stdout, stderr bytes.Buffer
	handled, code := dispatchError(fmtErrorf("account-1 has no stored credentials: %w", ErrSwitch), false, &stdout, &stderr)
	if !handled || code != 1 {
		t.Fatalf("got handled=%v code=%d, want handled=true code=1", handled, code)
	}
	if !strings.Contains(stderr.String(), "Error:") {
		t.Fatalf("expected a red Error: line on stderr, got %q", stderr.String())
	}
}

func TestDispatchErrorJSONModeWritesEnvelopeToStdout(t *testing.T) {
	var stdout, stderr bytes.Buffer
	handled, code := dispatchError(fmtErrorf("boom: %w", ErrConfig), true, &stdout, &stderr)
	if !handled || code != 1 {
		t.Fatalf("got handled=%v code=%d", handled, code)
	}
	if !strings.Contains(stdout.String(), `"schemaVersion"`) {
		t.Fatalf("expected a JSON envelope on stdout in JSON mode, got %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected stdout to stay the only JSON output; stderr should be empty in JSON error mode, got %q", stderr.String())
	}
}

func TestDispatchErrorUnhandledErrorPassesThrough(t *testing.T) {
	var stdout, stderr bytes.Buffer
	handled, _ := dispatchError(fmtErrorf("not a domain error"), false, &stdout, &stderr)
	if handled {
		t.Fatal("expected an error that does not wrap ErrClaudeSwitch to be reported as unhandled")
	}
}

func fmtErrorf(format string, a ...any) error {
	return fmt.Errorf(format, a...)
}

func TestExitCodeErrorDoesNotAffectExistingCommands(t *testing.T) {
	newTestSwitcher(t)
	var stdout, stderr bytes.Buffer
	code := RunCLI([]string{"cswap", "switch"}, &stdout, &stderr)
	// No accounts managed yet: this must still be a plain ClaudeSwitchError
	// handled the existing way (exit 1), not accidentally routed through
	// the new exitCodeError path.
	if code != 1 {
		t.Fatalf("got exit code %d, stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}
