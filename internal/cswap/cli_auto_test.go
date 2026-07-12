package cswap

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestHumanEmitColorsSwitchEventAccent(t *testing.T) {
	var buf bytes.Buffer
	emit := humanEmit(&buf)
	emit(NewSwitchEvent("proactive", nil, nil, nil, false))
	got := buf.String()
	if !strings.Contains(got, Accent("Switched (none) -> ? (proactive)")) {
		t.Fatalf("got %q", got)
	}
}

func TestHumanEmitColorsErrorEventYellowed(t *testing.T) {
	var buf bytes.Buffer
	emit := humanEmit(&buf)
	emit(NewErrorEvent("boom", true))
	got := buf.String()
	if !strings.Contains(got, Yellowed("error: boom (will retry)")) {
		t.Fatalf("got %q", got)
	}
}

func TestHumanEmitColorsPollEventDimmed(t *testing.T) {
	var buf bytes.Buffer
	emit := humanEmit(&buf)
	emit(NewPollEvent(nil, nil, nil, 90.0, nil, nil))
	got := buf.String()
	if !strings.Contains(got, Dimmed("poll: no active account")) {
		t.Fatalf("got %q", got)
	}
}

func TestJsonlEmitWritesOneCompactLinePerEvent(t *testing.T) {
	var buf bytes.Buffer
	emit := jsonlEmit(&buf)
	emit(NewNoSwitchEvent("below-threshold", "10% < 90%"))
	line := strings.TrimRight(buf.String(), "\n")
	if strings.Count(buf.String(), "\n") != 1 {
		t.Fatalf("expected exactly one line, got %q", buf.String())
	}
	if strings.Contains(line, "\n  ") {
		t.Fatalf("expected compact (non-indented) JSON, got %q", line)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(line), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["reason"] != "below-threshold" {
		t.Fatalf("got %#v", decoded)
	}
}

func TestAutoOnceNoActiveAccountExitsTwo(t *testing.T) {
	newTestSwitcher(t)
	var stdout, stderr bytes.Buffer
	code := RunCLI([]string{"cswap", "auto", "--once"}, &stdout, &stderr)
	if code != int(TickNoAction) {
		t.Fatalf("got exit code %d, stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestAutoOnceJSONEmitsPollEventLine(t *testing.T) {
	newTestSwitcher(t)
	var stdout, stderr bytes.Buffer
	code := RunCLI([]string{"cswap", "auto", "--once", "--json"}, &stdout, &stderr)
	if code != int(TickNoAction) {
		t.Fatalf("got exit code %d, stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"event":"poll"`) {
		t.Fatalf("expected a poll event line, got stdout=%q", stdout.String())
	}
}

func TestAutoThresholdFlagOverridesSettings(t *testing.T) {
	s := newTestSwitcher(t)
	if err := s.SetupDirectories(); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := RunCLI([]string{"cswap", "auto", "--once", "--json", "--threshold", "75"}, &stdout, &stderr)
	if code != int(TickNoAction) {
		t.Fatalf("got exit code %d, stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"threshold":75`) {
		t.Fatalf("expected the overridden threshold in the poll event, got stdout=%q", stdout.String())
	}
}

// Loop-mode signal handling (newAutoCommand's RunE registers only
// syscall.SIGTERM locally, deliberately not os.Interrupt, so that RunCLI's
// existing top-level os.Interrupt handler in cli.go remains the sole Ctrl-C
// path for every command including this one) is verified by code review,
// not by an automated test here. All tests in this file exercise --once,
// which never reaches that signal.Notify call. This matches this codebase's
// general practice elsewhere (see autoswitch_loop_test.go's
// TestRunLoopStopsPromptlyAndReturnsZero and friends): engine.Stop()'s
// graceful drain is unit-tested by calling Stop() directly, never by
// delivering a real OS signal in-process, since real signal delivery to a
// shared test binary is inherently timing-sensitive and not this
// codebase's style. A real SIGTERM-to-a-running-binary smoke test is the
// appropriate substitute and is run manually as part of this fix.

func TestAutoIncludeApiKeyAccountsFlagPair(t *testing.T) {
	// --include-api-key-accounts and --no-include-api-key-accounts must
	// each parse without error and must not both be settable at once in
	// a way that silently picks one; this test only confirms both flags
	// exist and parse, not the full merge behavior (already covered by
	// Plan 8's MergedWithCli tests).
	newTestSwitcher(t)
	var stdout, stderr bytes.Buffer
	code := RunCLI([]string{"cswap", "auto", "--once", "--include-api-key-accounts"}, &stdout, &stderr)
	if code != int(TickNoAction) {
		t.Fatalf("got exit code %d, stderr=%q", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	code = RunCLI([]string{"cswap", "auto", "--once", "--no-include-api-key-accounts"}, &stdout, &stderr)
	if code != int(TickNoAction) {
		t.Fatalf("got exit code %d, stderr=%q", code, stderr.String())
	}
}
