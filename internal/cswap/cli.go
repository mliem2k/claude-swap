package cswap

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
)

// version mirrors __version__: the Go build has no pyproject.toml to read,
// so this is an ldflags-overridable var instead (build with
// -ldflags "-X github.com/mliem2k/claude-swap/internal/cswap.version=X.Y.Z").
// "dev" is the fallback for a plain `go build`/`go run`.
var version = "dev"

// NewRootCommand builds the cobra command tree. Mirrors cli.py's argparse
// setup: a root command carrying the global --debug flag, with each verb
// (add, add-token, list, status, switch, remove, purge, config, auto,
// upgrade, export, import, run) as a subcommand.
func NewRootCommand() *cobra.Command {
	var debug bool

	var menubarFlag bool

	root := &cobra.Command{
		Use:     "cswap",
		Short:   "Switch between multiple Claude Code accounts",
		Version: version,
		// SilenceErrors/SilenceUsage: this plan's own dispatchError handles
		// the Error:/JSON-envelope/exit-code contract; cobra's default
		// error printing would produce a second, differently-formatted
		// message mirroring nothing in Python.
		SilenceErrors: true,
		SilenceUsage:  true,
		// Args/RunE: with no subcommands registered yet (addCommands is an
		// empty stub until Tasks 4-7), cobra treats root as non-runnable by
		// default, which skips its own "Usage:" block on help and skips its
		// legacyArgs unknown-command check entirely (both are gated on
		// HasSubCommands()/Runnable(), see cobra's command.go). Args:
		// cobra.NoArgs plus a RunE that just shows help makes root runnable
		// now (bare `cswap` prints full usage) and gives a stray positional
		// arg (`cswap not-a-real-command`) the same "unknown command %q for
		// %q" error legacyArgs would produce once real subcommands exist,
		// which isUsageError below already expects.
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Mirrors Python main()'s `args.menubar` dispatch, checked
			// before the TUI's own bare-invocation TTY gate (cswap
			// --menubar opens the macOS menu bar app even when stdout
			// isn't a terminal, e.g. launched from Finder/LaunchAgent).
			if menubarFlag {
				s, err := newSwitcher(debug)
				if err != nil {
					return err
				}
				return runMenubar(s)
			}
			if isInteractiveTerminal(os.Stdin, os.Stdout) {
				s, err := newSwitcher(debug)
				if err != nil {
					return err
				}
				return &exitCodeError{code: runTUI(s, "dashboard")}
			}
			// Mirrors main()'s "no command given" parser.error(): a bare,
			// non-interactive invocation (piped/scripted, not a real
			// terminal) is a usage error, not a quiet success. Exit 2 like
			// every other usage error, not cmd.Help()'s exit 0.
			return &cliUsageError{"no command given, try 'cswap help'"}
		},
	}
	root.PersistentFlags().BoolVar(&debug, "debug", false, "enable debug logging")
	root.Flags().BoolVar(&menubarFlag, "menubar", false, "open the macOS menu bar app")
	// The Go runtime version is display-only, baked into the template
	// string itself (not appended to the `version` package var), since
	// that var also feeds CheckForUpdate/RunSelfUpgrade's semver parsing
	// and must stay a bare "X.Y.Z".
	root.SetVersionTemplate(fmt.Sprintf("{{.Version}} (%s)\n", runtime.Version()))

	addCommands(root)
	root.PersistentPostRunE = func(cmd *cobra.Command, args []string) error {
		// Mirrors main()'s final `if not args.purge and not args.upgrade
		// and not args.json:` block. cobra only calls PersistentPostRunE
		// after a nil-returning RunE (confirmed by reading cobra's own
		// execute()), so a failed command already skips this the same way
		// Python's try block only reaches the check on success. auto's
		// RunE always returns a non-nil *exitCodeError even on a
		// successful tick, so this never fires for auto either, matching
		// Python's _auto_command being dispatched via an early return in
		// main() before this code is reached there too, no special-case
		// needed for auto specifically.
		if cmd.Name() == "purge" || cmd.Name() == "upgrade" {
			return nil
		}
		jsonOutput, _ := cmd.Flags().GetBool("json")
		if jsonOutput {
			return nil
		}
		if msg := CheckForUpdate(version); msg != "" {
			fmt.Fprintln(cmd.OutOrStdout(), "\n"+Muted(msg))
		}
		return nil
	}
	return root
}

// newSwitcher mirrors the root-check-then-construct sequence every cli.py
// dispatch path repeats: refuse root (outside a container), then build the
// switcher with the shared --debug flag.
func newSwitcher(debug bool) (*ClaudeAccountSwitcher, error) {
	if isRunningAsRoot() {
		s, err := NewClaudeAccountSwitcher(debug)
		if err == nil && !s.IsRunningInContainer() {
			return nil, errors.New("do not run this script as root (unless running in a container)")
		}
		if err != nil {
			return nil, err
		}
		return s, nil
	}
	return NewClaudeAccountSwitcher(debug)
}

// exitCodeError lets a command signal an exact process exit code that
// isn't cobra's own error-vs-success convention (0 or 1): `cswap auto
// --once`'s exit code is the TickOutcome value itself (0-3), a normal
// decision outcome, not necessarily a failure. No existing command
// returns this type; RunCLI checks for it before falling through to the
// generic error/usage-error handling below, so this is purely additive.
type exitCodeError struct{ code int }

func (e *exitCodeError) Error() string { return "" }

// dispatchError mirrors cli.py main()'s `except ClaudeSwitchError` /
// unhandled-exception split. A handled error (wrapping ErrClaudeSwitch)
// gets the "Error: <msg>" stderr line (or, in JSON mode, a JSON envelope
// on stdout so stdout stays pure JSON) and exit code 1; an unhandled error
// (a genuine bug, not a modeled domain failure) is reported as such via
// handled=false, so the caller can let it surface distinctly rather than
// silently mislabel a bug as a normal handled failure.
func dispatchError(err error, jsonOutput bool, stdout, stderr io.Writer) (handled bool, exitCode int) {
	if !errors.Is(err, ErrClaudeSwitch) {
		return false, 1
	}
	if jsonOutput {
		envelope := ErrorEnvelope(err)
		data, _ := json.MarshalIndent(envelope, "", "  ")
		fmt.Fprintln(stdout, string(data))
	} else {
		fmt.Fprintf(stderr, "Error: %s\n", err.Error())
	}
	return true, 1
}

// wantsMenubar reports whether cliArgs requests the --menubar flag, checked
// before RunCLI decides how to execute the command. The menu bar path must
// run root.Execute() on the calling goroutine rather than a spawned one (see
// RunCLI), so this has to be known before that choice is made, without
// actually invoking cobra's parser/RunE.
func wantsMenubar(cliArgs []string) bool {
	for _, arg := range cliArgs {
		if arg == "--menubar" || strings.HasPrefix(arg, "--menubar=") {
			return true
		}
	}
	return false
}

// resolveExitCode mirrors cli.py main()'s outer try/except and its
// documented exit codes: 0 success, 1 a handled ClaudeSwitchError, 2 an
// argparse-style usage error (unknown command, bad flags; cobra's own
// convention, matches argparse's default). Shared by both of RunCLI's
// execution paths (spawned-with-Ctrl-C and synchronous-for-menubar) so the
// error-to-exit-code mapping can't drift between them.
func resolveExitCode(err error, root *cobra.Command, cliArgs []string, stdout, stderr io.Writer) int {
	if err == nil {
		return 0
	}
	var ec *exitCodeError
	if errors.As(err, &ec) {
		return ec.code
	}
	jsonOutput := false
	if target, _, ferr := root.Find(cliArgs); ferr == nil && target != nil {
		jsonOutput, _ = target.Flags().GetBool("json")
	}
	if handled, code := dispatchError(err, jsonOutput, stdout, stderr); handled {
		return code
	}
	if isUsageError(err) {
		fmt.Fprintln(stderr, err.Error())
		return 2
	}
	fmt.Fprintln(stderr, err.Error())
	return 1
}

// RunCLI runs the full command line pipeline and returns the process exit
// code. Exported so main() and tests share one code path without main()
// calling os.Exit directly (which tests can't observe).
func RunCLI(args []string, stdout, stderr io.Writer) int {
	root := NewRootCommand()
	cliArgs := translateLegacyArgs(args[1:])
	root.SetArgs(cliArgs)
	root.SetOut(stdout)
	root.SetErr(stderr)

	// The menu bar's native event loop (Cocoa's NSApplication run loop on
	// macOS) must execute on the real OS main thread: systray's own init()
	// locks whatever goroutine runs it first (runtime.LockOSThread()) to its
	// OS thread, which is goroutine 1 (main()'s goroutine, guaranteed to
	// start on OS thread 0) precisely because init() functions run there
	// before main() begins. Running root.Execute() on a newly spawned
	// goroutine, as the general path below does to race against Ctrl-C,
	// hands the Cocoa call to an unlocked, arbitrary OS thread instead,
	// which crashes with a native SIGTRAP the instant the menu bar app
	// tries to enter its run loop. So this path calls root.Execute()
	// directly, keeping it on the caller's goroutine (real goroutine 1 when
	// invoked from main()). No Ctrl-C select: systray's own loop already
	// owns process termination (Quit menu item, Cmd+Q), matching how a
	// backgrounded menu bar app is normally stopped.
	if wantsMenubar(cliArgs) {
		return resolveExitCode(root.Execute(), root, cliArgs, stdout, stderr)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	defer signal.Stop(sigCh)
	done := make(chan error, 1)
	go func() { done <- root.Execute() }()

	select {
	case <-sigCh:
		// Mirrors cli.py's `except KeyboardInterrupt`: a clean cancellation
		// note instead of Go's default abrupt SIGINT termination.
		fmt.Fprintln(stdout, "\nOperation cancelled")
		return 130
	case err := <-done:
		return resolveExitCode(err, root, cliArgs, stdout, stderr)
	}
}

// cliUsageError mirrors an argparse parser.error() call: a flag-combination
// validation failure a command's own RunE detects (not cobra's own arg/flag
// parsing), which must still exit 2 and print to stderr like every other
// usage error, matching Python's argparse.error() convention (checked
// before any switcher construction or side effect, since these are pure
// static-validation errors).
type cliUsageError struct{ msg string }

func (e *cliUsageError) Error() string { return e.msg }

// isUsageError distinguishes a cobra/pflag argument-parsing failure (no
// such command, unknown flag) or a cliUsageError from a genuine unhandled
// runtime error, so RunCLI can mirror argparse's exit code 2 for the
// former only.
func isUsageError(err error) bool {
	var ue *cliUsageError
	if errors.As(err, &ue) {
		return true
	}
	msg := err.Error()
	if len(msg) == 0 {
		return false
	}
	if contains(msg, "unknown command") || contains(msg, "unknown flag") || contains(msg, "unknown shorthand flag") {
		return true
	}
	// cobra.ExactArgs/MinimumNArgs/MaximumNArgs/RangeArgs all format their
	// error around "N arg(s)" (args.go in the cobra module); matching on
	// that substring catches every arg-count validator used across this
	// CLI's subcommands without needing a case per validator.
	return contains(msg, "arg(s)")
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && indexOf(s, substr) >= 0
}

func indexOf(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// addCommands registers every subcommand on root: add, add-token, list,
// status, switch, remove, purge, config, auto, upgrade, export, import,
// run, tui, watch.
func addCommands(root *cobra.Command) {
	root.AddCommand(newAddCommand())
	root.AddCommand(newAddTokenCommand())
	root.AddCommand(newListCommand())
	root.AddCommand(newStatusCommand())
	root.AddCommand(newSwitchCommand())
	root.AddCommand(newRemoveCommand())
	root.AddCommand(newPurgeCommand())
	root.AddCommand(newConfigCommand())
	root.AddCommand(newAutoCommand())
	root.AddCommand(newUpgradeCommand())
	root.AddCommand(newExportCommand())
	root.AddCommand(newImportCommand())
	root.AddCommand(newRunCommand())
	root.AddCommand(newTUICommand())
	root.AddCommand(newWatchCommand())
}

// stdinOverride lets tests inject stdin without changing every command's
// signature; nil (the default) means "use the real os.Stdin". Tests must
// reset this to nil when done (see runCLIWithStdin below).
var stdinOverride io.Reader

// runCLIWithStdin is a test helper: RunCLI with a caller-supplied stdin,
// for exercising interactive confirm prompts (add/remove/purge/list's
// first-run setup) without touching the real terminal.
func runCLIWithStdin(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	stdinOverride = stdin
	defer func() { stdinOverride = nil }()
	return RunCLI(args, stdout, stderr)
}

// cliStdin returns the real os.Stdin, or a test override when set.
func cliStdin() io.Reader {
	if stdinOverride != nil {
		return stdinOverride
	}
	return os.Stdin
}
