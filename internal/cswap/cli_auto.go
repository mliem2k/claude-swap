package cswap

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

// humanEmit mirrors human_emit: prints an HH:MM:SS-stamped, color-coded
// line per event.
func humanEmit(out io.Writer) func(AutoSwitchEvent) {
	return func(event AutoSwitchEvent) {
		stamp := time.Now().Format("15:04:05")
		line := event.Human()
		switch event.Kind() {
		case "switch":
			line = Accent(line)
		case "error", "account-quarantined":
			line = Yellowed(line)
		case "poll", "no-switch", "sleep":
			line = Dimmed(line)
		}
		fmt.Fprintf(out, "%s  %s\n", stamp, line)
	}
}

// jsonlEmit mirrors jsonl_emit: one compact JSON line per event, for
// streaming/piping. Deliberately NOT printJSON's indented shape, this is
// a line-oriented log, not a one-shot payload.
func jsonlEmit(out io.Writer) func(AutoSwitchEvent) {
	return func(event AutoSwitchEvent) {
		data, _ := json.Marshal(event.ToJSON())
		fmt.Fprintln(out, string(data))
	}
}

// newAutoCommand mirrors _auto_command's argparse setup and dispatch.
func newAutoCommand() *cobra.Command {
	var jsonOutput, once, dryRun bool
	var includeAPIKeyAccounts, noIncludeAPIKeyAccounts bool
	var interval, threshold, cooldown float64
	var model string

	cmd := &cobra.Command{
		Use:   "auto",
		Short: "Automatically switch accounts before hitting rate limits",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			debug, _ := cmd.Flags().GetBool("debug")
			s, err := newSwitcher(debug)
			if err != nil {
				return err
			}

			var overrides CliOverrides
			if cmd.Flags().Changed("threshold") {
				overrides.Threshold = &threshold
			}
			if cmd.Flags().Changed("interval") {
				overrides.IntervalSeconds = &interval
			}
			if cmd.Flags().Changed("cooldown") {
				overrides.CooldownSeconds = &cooldown
			}
			if cmd.Flags().Changed("model") {
				overrides.Model = &model
			}
			switch {
			case cmd.Flags().Changed("include-api-key-accounts"):
				overrides.IncludeAPIKeyAccounts = &includeAPIKeyAccounts
			case cmd.Flags().Changed("no-include-api-key-accounts"):
				forceFalse := false
				overrides.IncludeAPIKeyAccounts = &forceFalse
			}
			settings := MergedWithCli(LoadSettings(s.BackupDir), overrides)

			stdout := cmd.OutOrStdout()
			var onEvent func(AutoSwitchEvent)
			if jsonOutput {
				onEvent = jsonlEmit(stdout)
			} else {
				onEvent = humanEmit(stdout)
			}
			engine := NewAutoSwitchEngine(s, settings, onEvent, dryRun, "", nil)

			if once {
				outcome := engine.Tick()
				return &exitCodeError{code: int(outcome)}
			}

			// Only SIGTERM is handled here, mirroring Python's
			// signal.signal(SIGTERM, lambda *_: engine.stop()): a graceful
			// drain, letting RunLoop finish its current wait and return
			// cleanly. Ctrl-C (SIGINT) is deliberately NOT registered here:
			// Python has no explicit SIGINT handler either, it relies on
			// the default KeyboardInterrupt unwind, an abrupt stop, not a
			// coordinated engine.Stop() drain. RunCLI's own top-level
			// os.Interrupt handler (cli.go) already provides that abrupt
			// stop for every command, including this one; registering a
			// second, local os.Interrupt handler here would only race
			// against it and lose (RunCLI's channel fires on a bare
			// receive; this one requires waking a goroutine, calling
			// Stop(), and waiting for RunLoop to notice), so
			// engine.Stop()'s graceful drain would never actually run for
			// SIGINT anyway. The one known, accepted cosmetic gap from
			// this: Python's Ctrl-C path prints "Auto-switch stopped" (to
			// stderr in --json mode) where RunCLI's shared top-level
			// handler prints "Operation cancelled" to stdout regardless of
			// --json, matching every other command's Ctrl-C behavior
			// rather than duplicating a per-command message here.
			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGTERM)
			defer signal.Stop(sigCh)
			go func() {
				<-sigCh
				engine.Stop()
			}()

			if !jsonOutput {
				dryRunSuffix := ""
				if dryRun {
					dryRunSuffix = " (dry-run)"
				}
				fmt.Fprintln(stdout, Dimmed(fmt.Sprintf(
					"Auto-switch running: threshold %.0f%%, every %.0fs%s. Ctrl-C to stop",
					settings.Threshold, settings.IntervalSeconds, dryRunSuffix,
				)))
			}
			return &exitCodeError{code: engine.RunLoop()}
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "emit one JSON event per line instead of human-readable output")
	cmd.Flags().BoolVar(&once, "once", false, "evaluate a single tick and exit")
	cmd.Flags().Float64Var(&interval, "interval", 0, "poll interval in seconds")
	cmd.Flags().Float64Var(&threshold, "threshold", 0, "switch threshold percentage")
	cmd.Flags().Float64Var(&cooldown, "cooldown", 0, "minimum seconds between proactive switches")
	cmd.Flags().StringVar(&model, "model", "", "comma-separated model names or \"all\"")
	cmd.Flags().BoolVar(&includeAPIKeyAccounts, "include-api-key-accounts", false, "include API-key accounts as switch candidates")
	cmd.Flags().BoolVar(&noIncludeAPIKeyAccounts, "no-include-api-key-accounts", false, "exclude API-key accounts as switch candidates")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "evaluate and report without switching")
	return cmd
}
