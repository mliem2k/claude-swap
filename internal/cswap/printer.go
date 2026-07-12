package cswap

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// ANSI escape codes, mirroring printer.py's module constants.
const (
	ansiReset  = "\033[0m"
	ansiBold   = "\033[1m"
	ansiDim    = "\033[2m"
	ansiRed    = "\033[31m"
	ansiYellow = "\033[33m"
	ansiAccent = "\033[38;5;173m" // warm salmon/terracotta
	ansiMuted  = "\033[38;5;250m" // soft gray, readable but quieter than normal
)

// timeNowFunc is a seam so tests can be deterministic about "now" without
// FormatAge itself taking a clock parameter (matching Python's format_age,
// which calls time.time() directly).
var timeNowFunc = time.Now

// colorsEnabledCache mirrors the module-level _colors_enabled: nil until the
// first call to ColorsEnabled, which probes and caches the result.
var colorsEnabledCache *bool

// detectColorSupport mirrors _detect_color_support.
func detectColorSupport() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if os.Getenv("FORCE_COLOR") != "" {
		return true
	}
	fi, err := os.Stdout.Stat()
	if err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	if !enableWindowsVT() {
		return false
	}
	if os.Getenv("TERM") == "dumb" {
		return false
	}
	return true
}

// ColorsEnabled mirrors colors_enabled: whether color output is active.
// Caches on first call.
func ColorsEnabled() bool {
	if colorsEnabledCache == nil {
		v := detectColorSupport()
		colorsEnabledCache = &v
	}
	return *colorsEnabledCache
}

// ForceColor mirrors force_color: temporarily forces colored output on,
// returning a restore func in place of Python's context manager (matching
// this codebase's existing restore-func convention, e.g. swapSecurity).
// Used by the TUI when capturing CLI output into a buffer that isn't a real
// tty but still wants ANSI codes emitted for its own re-rendering.
func ForceColor() func() {
	saved := colorsEnabledCache
	v := true
	colorsEnabledCache = &v
	return func() { colorsEnabledCache = saved }
}

// style mirrors _style: applies ANSI codes to text if colors are enabled.
func style(text string, codes ...string) string {
	if !ColorsEnabled() {
		return text
	}
	return strings.Join(codes, "") + text + ansiReset
}

// Accent mirrors accent: warm accent color for important elements.
func Accent(text string) string { return style(text, ansiAccent) }

// Muted mirrors muted: slightly dimmer than normal, for usage stats, org tags.
func Muted(text string) string { return style(text, ansiMuted) }

// Dimmed mirrors dimmed: dim for tertiary info, tree connectors, hints.
func Dimmed(text string) string { return style(text, ansiDim) }

// Bolded mirrors bolded: bold (no color) for structure.
func Bolded(text string) string { return style(text, ansiBold) }

// BoldAccent mirrors bold_accent: bold + accent for key markers like (active).
func BoldAccent(text string) string { return style(text, ansiBold, ansiAccent) }

// Yellowed mirrors yellowed: yellow for warning-toned text (string form;
// Warning below prints).
func Yellowed(text string) string { return style(text, ansiYellow) }

// Error mirrors error(): prints an error message (red) to stderr.
func Error(msg string) {
	fmt.Fprintln(os.Stderr, style(msg, ansiRed))
}

// Warning mirrors warning(): prints a warning message (yellow) to stdout.
func Warning(msg string) {
	fmt.Println(style(msg, ansiYellow))
}

// entrypointLabels mirrors _ENTRYPOINT_LABELS.
var entrypointLabels = map[string]string{
	"cli":            "CLI",
	"claude-vscode":  "VS Code",
	"claude-desktop": "Desktop",
	"sdk-cli":        "SDK",
	"sdk-ts":         "SDK",
	"sdk-py":         "SDK",
	"mcp":            "MCP",
	"local-agent":    "Agent",
	"remote":         "Remote",
}

// ideShortNames mirrors _IDE_SHORT_NAMES.
var ideShortNames = map[string]string{
	"Visual Studio Code": "VS Code",
}

// EntrypointLabel mirrors entrypoint_label: a human-readable label for a
// Claude Code entrypoint.
func EntrypointLabel(entrypoint string) string {
	if label, ok := entrypointLabels[entrypoint]; ok {
		return label
	}
	return entrypoint
}

// IdeShortName mirrors ide_short_name: a short display name for an IDE.
func IdeShortName(ideName string) string {
	if short, ok := ideShortNames[ideName]; ok {
		return short
	}
	return ideName
}

// AbbreviatePath mirrors abbreviate_path: replaces the user's home directory
// prefix with ~.
func AbbreviatePath(path string) string {
	home := homeDir()
	if strings.HasPrefix(path, home) {
		return "~" + path[len(home):]
	}
	return path
}

// FormatAge mirrors format_age: a millisecond epoch timestamp as a
// human-readable age.
func FormatAge(startedAtMs int64) string {
	elapsed := timeNowFunc().Unix() - startedAtMs/1000
	switch {
	case elapsed < 60:
		return "just now"
	case elapsed < 3600:
		return fmt.Sprintf("%dm ago", elapsed/60)
	case elapsed < 86400:
		return fmt.Sprintf("%dh ago", elapsed/3600)
	default:
		return fmt.Sprintf("%dd ago", elapsed/86400)
	}
}

// nowUnixForTest exists so tests can read the same clock FormatAge uses
// without FormatAge itself taking a clock parameter (Python's format_age
// doesn't either; it calls time.time() directly).
func nowUnixForTest() int64 { return timeNowFunc().Unix() }
