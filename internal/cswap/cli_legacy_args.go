package cswap

// legacyFlagToVerb mirrors _SUBCOMMAND_FLAGS's reverse direction: the
// hidden top-level flags cli.py's argparse still accepts (`cswap --list`)
// map to the same verb the modern subcommand form uses (`cswap list`).
// `--menubar` is deliberately absent here: it is wired as its own root
// flag directly (cli.go's `root.Flags().BoolVar(&menubarFlag, ...)`), not
// a verb with a subcommand, so it needs no translation.
var legacyFlagToVerb = map[string]string{
	"--list":           "list",
	"--ls":             "list",
	"--status":         "status",
	"--add-account":    "add",
	"--add-token":      "add-token",
	"--remove-account": "remove",
	"--purge":          "purge",
	"--export":         "export",
	"--import":         "import",
	"--upgrade":        "upgrade",
	"--tui":            "tui",
	"--watch":          "watch",
}

// translateLegacyArgs mirrors _translate_subcommand: rewrites a legacy
// flag-style invocation into the modern subcommand form cobra expects.
// `--switch` (bare) becomes `switch`; `--switch-to VALUE` becomes
// `switch VALUE` (mirroring Python's positional-vs-flag special case for
// switch, since a bare `switch` command means "rotate" and `switch VALUE`
// means "jump to VALUE"). Any argument not recognized as a legacy flag
// (including an already-modern subcommand name) passes through unchanged.
func translateLegacyArgs(args []string) []string {
	if len(args) == 0 {
		return args
	}
	switch args[0] {
	case "--switch":
		return append([]string{"switch"}, args[1:]...)
	case "--switch-to":
		return append([]string{"switch"}, args[1:]...)
	}
	if verb, ok := legacyFlagToVerb[args[0]]; ok {
		return append([]string{verb}, args[1:]...)
	}
	return args
}
