package cswap

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// newSwitchCommand mirrors cli.py's --switch / --switch-to dispatch: a
// bare `switch` rotates (Switch), `switch IDENTIFIER` jumps (SwitchTo).
func newSwitchCommand() *cobra.Command {
	var jsonOutput, force bool
	var strategy, modelFlag string
	cmd := &cobra.Command{
		Use:   "switch [identifier]",
		Short: "Switch the active Claude account",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Mirrors main()'s parser.error() validation block: these flag
			// combinations are meaningless (or actively misleading, since
			// nothing reads them) outside a bare-rotation switch, so reject
			// loudly instead of silently ignoring the flag, matching
			// Python's argparse-level checks exactly (exit 2, no side
			// effects, checked before the switcher is even constructed).
			if len(args) == 1 && strategy != "" {
				return &cliUsageError{"--strategy can only be used with bare 'switch'"}
			}
			if modelFlag != "" && strategy == "" {
				return &cliUsageError{"--model can only be used with 'switch --strategy best' or 'switch --strategy next-available'"}
			}
			if force && len(args) == 0 {
				return &cliUsageError{"--force can only be used with 'switch <identifier>' (not a bare rotation)"}
			}

			debug, _ := cmd.Flags().GetBool("debug")
			s, err := newSwitcher(debug)
			if err != nil {
				return err
			}

			if len(args) == 1 {
				// Mirrors Python's `if not json_output:` guard around the
				// interactive ambiguous-email prompt: JSON mode never
				// prompts, falling through to SwitchTo's own informative
				// ambiguous-match error instead (see SwitchTo's doc
				// comment on its disambiguate parameter).
				var disambiguate func([]string) string
				if !jsonOutput {
					disambiguate = disambiguatePrompt(s, args[0], "Enter account number to switch to: ", cliStdin(), cmd.OutOrStdout())
				}
				res, err := s.SwitchTo(args[0], force, disambiguate)
				if err != nil {
					return err
				}
				return renderSwitchResult(cmd, res, jsonOutput, nil, "")
			}

			// Only the usage-aware strategies read model limits: --model
			// wins; otherwise the persistent autoswitch.model setting
			// applies (mirrors main()'s "models, model_source" branch:
			// --model given -> "cli", else the configured setting ->
			// "autoswitch.model" when it names any models, else no
			// source at all).
			var models []string
			var modelSource string
			if strategy != "" {
				if modelFlag != "" {
					for _, m := range strings.Split(modelFlag, ",") {
						if trimmed := strings.TrimSpace(m); trimmed != "" {
							models = append(models, trimmed)
						}
					}
					modelSource = "cli"
				} else {
					settings := LoadSettings(s.BackupDir)
					if settings.Model != nil {
						models = ParseModelNames(*settings.Model)
					}
					if len(models) > 0 {
						modelSource = "autoswitch.model"
					}
				}
			}
			res, err := s.Switch(strategy, models)
			if err != nil {
				return err
			}
			return renderSwitchResult(cmd, res, jsonOutput, models, modelSource)
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	cmd.Flags().BoolVar(&force, "force", false, "switch even if the live login is unmanaged or its backup is stale (identifier form only)")
	cmd.Flags().StringVar(&strategy, "strategy", "", "rotation strategy: best or next-available (bare switch only)")
	cmd.Flags().StringVar(&modelFlag, "model", "", "comma-separated model list, or \"all\" (bare switch only)")
	return cmd
}

// renderSwitchResult mirrors main()'s post-switch(_to) rendering: JSON
// payload (with "models"/"modelSource" keys appended when models is
// non-empty, matching cli.py's `if payload is not None and models:
// payload["models"] = list(models); payload["modelSource"] = model_source`)
// or the human message plus warnings, plus the "Using configured model
// limits" notice switch() prints itself in Python (see switch()'s own
// pre-switch announcement in switcher_switch.go).
func renderSwitchResult(cmd *cobra.Command, res *SwitchResult, jsonOutput bool, models []string, modelSource string) error {
	if jsonOutput {
		payload := switchResultToJSON(res)
		if len(models) > 0 {
			modelsAny := make([]any, len(models))
			for i, m := range models {
				modelsAny[i] = m
			}
			payload["models"] = modelsAny
			payload["modelSource"] = modelSource
		}
		printJSON(cmd.OutOrStdout(), payload)
		return nil
	}
	if len(models) > 0 {
		cmd.Println(Dimmed(fmt.Sprintf("Using configured model limits: %s (from %s)", strings.Join(models, ", "), modelSourceLabel(modelSource))))
	}
	cmd.Println(res.Message)
	for _, w := range res.Warnings {
		cmd.Println(w)
	}
	return nil
}

// modelSourceLabel mirrors the "(from --model)"/"(from autoswitch.model)"
// wording switch() uses in Python (switcher.py's pre-switch notice).
func modelSourceLabel(modelSource string) string {
	if modelSource == "cli" {
		return "--model"
	}
	return modelSource
}

// switchResultToJSON mirrors _switch_result_from_op / _switch_noop's shared
// payload shape (both return the same 8 keys; switch()/switch_to() return
// this dict in json_output mode). warnings is always a list, present even
// when empty, matching Python's `warnings or []` (never conditionally
// omitted the way duplicateAccountWarnings/lockstepUsageWarnings are on
// the list/status payloads, since those two are genuinely additive-only
// fields in Python while this one is not).
func switchResultToJSON(res *SwitchResult) map[string]any {
	payload := map[string]any{
		"schemaVersion": JSONSchemaVersion,
		"switched":      res.Switched,
		"strategy":      res.Strategy,
		"reason":        res.Reason,
		"message":       res.Message,
	}
	if res.From != nil {
		payload["from"] = AccountRefDict(res.From.Number, res.From.Email)
	} else {
		payload["from"] = nil
	}
	if res.To != nil {
		payload["to"] = AccountRefDict(res.To.Number, res.To.Email)
	} else {
		payload["to"] = nil
	}
	warnings := make([]any, len(res.Warnings))
	for i, w := range res.Warnings {
		warnings[i] = w
	}
	payload["warnings"] = warnings
	return payload
}
