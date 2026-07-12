package cswap

import (
	"bytes"
	"strings"
	"testing"
)

func TestNewMenubarCommandReturnsErrorWithoutFactory(t *testing.T) {
	old := MenubarAppFactory
	MenubarAppFactory = nil
	defer func() { MenubarAppFactory = old }()

	// runMenubar (not the cobra root command itself, to avoid needing a
	// real switcher/backup dir in this unit test) must fail cleanly, not
	// panic, when the factory hasn't been wired (mirrors runTUI's own
	// TUIAppFactory nil-guard test coverage).
	err := runMenubar(nil)
	if err == nil {
		t.Fatal("expected an error when MenubarAppFactory is nil")
	}
}

func TestRunCLIMenubarWithoutFactoryPrintsErrorOnce(t *testing.T) {
	// Regression test for the double-print bug: runMenubar used to both
	// call Error() directly (writes straight to os.Stderr) AND return an
	// ErrClaudeSwitch-wrapped error that dispatchError also prints (to
	// RunCLI's own stderr param). That produced the message twice. This
	// drives the failure through the full `cswap --menubar` path (the
	// same one the reviewer exercised via a real binary run) and checks
	// both output surfaces combined contain the message exactly once.
	newTestSwitcher(t)

	old := MenubarAppFactory
	MenubarAppFactory = nil
	defer func() { MenubarAppFactory = old }()

	var stdout, stderr bytes.Buffer
	var code int
	osStderr := captureStderr(t, func() {
		code = RunCLI([]string{"cswap", "--menubar"}, &stdout, &stderr)
	})

	if code != 1 {
		t.Fatalf("got exit code %d, want 1; stdout=%q stderr=%q osStderr=%q", code, stdout.String(), stderr.String(), osStderr)
	}

	const want = "menu bar support is not wired up in this build"
	combined := osStderr + stderr.String()
	if got := strings.Count(combined, want); got != 1 {
		t.Fatalf("expected %q exactly once across os.Stderr + RunCLI stderr, got %d occurrences; osStderr=%q runCLIStderr=%q", want, got, osStderr, stderr.String())
	}
}

// TestWantsMenubarDetectsFlagForms is the regression test for the real bug:
// RunCLI used to always execute root.Execute() on a spawned goroutine (to
// select against Ctrl-C), which handed the menu bar's Cocoa native run loop
// to an OS thread that systray's own runtime.LockOSThread() (in its init(),
// which only locks whichever goroutine runs it first, i.e. goroutine 1) never
// locked, crashing with a native SIGTRAP the instant NSApplication's run
// loop started. The fix routes a --menubar invocation through root.Execute()
// directly on RunCLI's own caller goroutine instead; wantsMenubar is the
// pre-parse check that decides which path to take, so it must recognize
// every form cobra itself would parse as the flag.
func TestWantsMenubarDetectsFlagForms(t *testing.T) {
	cases := []struct {
		args []string
		want bool
	}{
		{[]string{"--menubar"}, true},
		{[]string{"--menubar=true"}, true},
		{[]string{"--debug", "--menubar"}, true},
		{[]string{}, false},
		{[]string{"list"}, false},
		{[]string{"add"}, false},
		// A value merely containing "menubar" (not the flag itself) must not
		// false-positive and route a normal subcommand onto the synchronous,
		// no-Ctrl-C path.
		{[]string{"switch", "menubar@example.com"}, false},
	}
	for _, c := range cases {
		if got := wantsMenubar(c.args); got != c.want {
			t.Errorf("wantsMenubar(%v) = %v, want %v", c.args, got, c.want)
		}
	}
}

func TestMenubarCommandRegistered(t *testing.T) {
	// cswap --menubar is a boolean flag on the root command (mirroring
	// Python's argparse --menubar), not a subcommand like `cswap tui`, so
	// it's probed via root.Flags().Lookup rather than root.Find.
	root := NewRootCommand()
	flag := root.Flags().Lookup("menubar")
	if flag == nil {
		t.Fatal("--menubar flag not registered on root command")
	}
	if flag.Value.Type() != "bool" {
		t.Fatalf("--menubar flag should be a bool, got %s", flag.Value.Type())
	}
}
