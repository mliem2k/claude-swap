package cswap

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// AutoSwitchSettings mirrors AutoSwitchSettings: validated auto-switch
// configuration. The 90% threshold default leaves headroom for the
// Keychain-propagation tail after a switch; hysteresis_pct keeps a
// borderline account from flapping back and forth between two nearly-tied
// candidates.
type AutoSwitchSettings struct {
	Threshold             float64
	IntervalSeconds       float64
	CooldownSeconds       float64
	HysteresisPct         float64
	Strategy              string
	IncludeAPIKeyAccounts bool
	UnhealthyTicks        int
	Model                 *string // nil mirrors Python's None
}

// DefaultAutoSwitchSettings mirrors AutoSwitchSettings()'s field defaults.
func DefaultAutoSwitchSettings() AutoSwitchSettings {
	return AutoSwitchSettings{
		Threshold:             90.0,
		IntervalSeconds:       60.0,
		CooldownSeconds:       300.0,
		HysteresisPct:         10.0,
		Strategy:              "best",
		IncludeAPIKeyAccounts: false,
		UnhealthyTicks:        3,
		Model:                 nil,
	}
}

// SettingSpec mirrors SettingSpec: the single source of truth for one
// setting's bounds, choices, default, and help text, used by both the
// lenient load-time clamp and the strict `config set` validation so the
// two validation paths cannot drift, exactly as in Python.
type SettingSpec struct {
	Section string
	JSONKey string
	Field   string // AutoSwitchSettings field this setting maps to, by Python-style snake_case name
	Kind    string // "float", "int", "bool", "choice", or "string"
	Lo, Hi  float64
	Choices []string
	Help    string
}

// Dotted mirrors the `dotted` property: "{section}.{json_key}", the key
// `cswap config` and settings.json addressing both use.
func (s SettingSpec) Dotted() string {
	return s.Section + "." + s.JSONKey
}

// Default mirrors the `default` property: reads the live
// DefaultAutoSwitchSettings() field this spec names, rather than
// duplicating the value as a separate literal.
func (s SettingSpec) Default() any {
	d := DefaultAutoSwitchSettings()
	switch s.Field {
	case "threshold":
		return d.Threshold
	case "interval_seconds":
		return d.IntervalSeconds
	case "cooldown_seconds":
		return d.CooldownSeconds
	case "hysteresis_pct":
		return d.HysteresisPct
	case "strategy":
		return d.Strategy
	case "include_api_key_accounts":
		return d.IncludeAPIKeyAccounts
	case "unhealthy_ticks":
		return d.UnhealthyTicks
	case "model":
		if d.Model == nil {
			return nil
		}
		return *d.Model
	}
	return nil
}

// settingSpecsOrder mirrors SETTING_SPECS's declaration order (Python
// dicts preserve insertion order; effective_settings/config list must
// iterate in this same order). This is the single registry every other
// function in this file reads bounds/choices/help/defaults from.
var settingSpecsOrder = []SettingSpec{
	{Section: "autoswitch", JSONKey: "threshold", Field: "threshold", Kind: "float", Lo: 50.0, Hi: 99.9,
		Help: "Switch when the binding 5h/7d window reaches this pct"},
	{Section: "autoswitch", JSONKey: "intervalSeconds", Field: "interval_seconds", Kind: "float", Lo: 15.0, Hi: 3600.0,
		Help: "Poll interval for the cswap auto loop, in seconds"},
	{Section: "autoswitch", JSONKey: "cooldownSeconds", Field: "cooldown_seconds", Kind: "float", Lo: 0.0, Hi: 86400.0,
		Help: "Minimum seconds between proactive switches"},
	{Section: "autoswitch", JSONKey: "hysteresisPct", Field: "hysteresis_pct", Kind: "float", Lo: 0.0, Hi: 50.0,
		Help: "A target must beat the active account by this many pct"},
	{Section: "autoswitch", JSONKey: "strategy", Field: "strategy", Kind: "choice", Choices: []string{"best"},
		Help: "How auto-switch picks the target account"},
	{Section: "autoswitch", JSONKey: "includeApiKeyAccounts", Field: "include_api_key_accounts", Kind: "bool",
		Help: "Allow rotating onto managed API-key accounts (bill per token)"},
	{Section: "autoswitch", JSONKey: "unhealthyTicks", Field: "unhealthy_ticks", Kind: "int", Lo: 1, Hi: 100,
		Help: "Consecutive failed polls before an account is unhealthy"},
	{Section: "autoswitch", JSONKey: "model", Field: "model", Kind: "string",
		Help: "Also switch on these models' weekly limits (e.g. Fable, Fable,Opus, or all)"},
}

// SettingSpecFor mirrors setting_spec: looks up a dotted key in the
// registry. Named SettingSpecFor, not SettingSpec, to avoid colliding
// with the SettingSpec type itself (this port's established convention
// for this exact kind of collision, see AccountRefDict in json_output.go).
func SettingSpecFor(dottedKey string) (SettingSpec, error) {
	for _, spec := range settingSpecsOrder {
		if spec.Dotted() == dottedKey {
			return spec, nil
		}
	}
	keys := make([]string, len(settingSpecsOrder))
	for i, spec := range settingSpecsOrder {
		keys[i] = spec.Dotted()
	}
	return SettingSpec{}, fmt.Errorf("unknown setting '%s'\nValid keys: %s: %w",
		dottedKey, strings.Join(keys, ", "), ErrConfig)
}

// SettingsPath mirrors settings_path: backupRoot/settings.json.
func SettingsPath(backupRoot string) string {
	return filepath.Join(backupRoot, "settings.json")
}

// SettingsSchemaVersion is this file's own schema version, following the
// same per-domain-constant convention as JSONSchemaVersion (json_output.go),
// StateSchemaVersion (autoswitch.go), and UsageSchemaVersion (usage_store.go).
const SettingsSchemaVersion = 1

// ParseModelNames mirrors parse_model_names: comma-split, trimmed,
// case-insensitive dedup with first spelling wins, "" -> empty.
func ParseModelNames(value string) []string {
	if value == "" {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, part := range strings.Split(value, ",") {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		lower := strings.ToLower(trimmed)
		if seen[lower] {
			continue
		}
		seen[lower] = true
		out = append(out, trimmed)
	}
	return out
}

// readSettingsRaw mirrors _read_raw: the forgiving reader used by
// LoadSettings/EffectiveSettings. A missing file is silent (not logged);
// any other read/parse/shape problem degrades to an empty map (every
// setting reverts to its default) rather than raising, and IS logged, so
// a corrupt settings.json never blocks the tool from running.
func readSettingsRaw(path string) map[string]any {
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Default().Warn(fmt.Sprintf("could not read %s (%v); using defaults", path, err))
		}
		return map[string]any{}
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		slog.Default().Warn(fmt.Sprintf("could not read %s (%v); using defaults", path, err))
		return map[string]any{}
	}
	return parsed
}

// clampedNumber mirrors _clamped's inner num(): rejects bool (Python
// checks isinstance(value, bool) before the numeric check, since bool is
// an int subclass in Python) and any non-numeric type, returning ok=false
// so the caller keeps the field at its default; otherwise clamps into
// [lo, hi]. JSON numbers always decode to float64 in Go's map[string]any,
// so only that type is handled.
func clampedNumber(value any, lo, hi float64) (float64, bool) {
	if _, isBool := value.(bool); isBool {
		return 0, false
	}
	n, ok := value.(float64)
	if !ok {
		return 0, false
	}
	if n < lo {
		n = lo
	}
	if n > hi {
		n = hi
	}
	return n, true
}

// clamped mirrors _clamped: forgiving load-time normalization, driven by
// settingSpecsOrder (the same registry ParseSettingValue's strict path
// uses), so bounds/choices/defaults can never drift between the two
// validation paths. Any field value of the wrong type or out of range
// reverts to that field's spec default; an unsupported choice value is
// logged as a warning and also reverts. Never raises.
func clamped(raw map[string]any) AutoSwitchSettings {
	out := DefaultAutoSwitchSettings()
	for _, spec := range settingSpecsOrder {
		val, present := raw[spec.JSONKey]
		if !present {
			continue
		}
		switch spec.Kind {
		case "float":
			if n, ok := clampedNumber(val, spec.Lo, spec.Hi); ok {
				assignSettingField(&out, spec.Field, n)
			}
		case "int":
			if n, ok := clampedNumber(val, spec.Lo, spec.Hi); ok {
				assignSettingField(&out, spec.Field, int(n))
			}
		case "bool":
			assignSettingField(&out, spec.Field, boolish(val))
		case "string":
			if s, ok := val.(string); ok && s != "" {
				assignSettingField(&out, spec.Field, s)
			}
		case "choice":
			s, ok := val.(string)
			if !ok || !containsString(spec.Choices, s) {
				slog.Default().Warn(fmt.Sprintf("settings.json: unsupported %s %v; using %v",
					spec.Dotted(), val, spec.Default()))
				continue
			}
			assignSettingField(&out, spec.Field, s)
		}
	}
	return out
}

// boolish mirrors Python's permissive bool(value) coercion for the bool
// kind (unlike every other kind, Python's _clamped never reverts a
// bool-kind value to the default for a "wrong" type; any JSON type is
// coerced via Python's generic truthiness: 0/""/null/empty
// list-or-object are false, everything else true). Go's map[string]any
// values from JSON are only ever bool/float64/string/[]any/
// map[string]any/nil, so those are the only cases that matter here. An
// earlier draft of this function took a second "already-computed bool"
// fallback parameter that was always false for any non-bool input
// (val.(bool) on a non-bool zero-values to false), which silently
// mismatched Python's true-for-non-empty-object/array truthiness; this
// version handles every JSON-decodable type directly instead.
func boolish(val any) bool {
	switch v := val.(type) {
	case bool:
		return v
	case float64:
		return v != 0
	case string:
		return v != ""
	case nil:
		return false
	case []any:
		return len(v) > 0
	case map[string]any:
		return len(v) > 0
	}
	return false
}

// containsString reports whether choices contains s.
func containsString(choices []string, s string) bool {
	for _, c := range choices {
		if c == s {
			return true
		}
	}
	return false
}

// assignSettingField mirrors Python's setattr(settings, spec.field,
// value) dynamic dispatch: sets the one AutoSwitchSettings field
// spec.Field names. A small, explicit switch rather than reflection,
// consistent with this codebase's convention of not using reflect
// anywhere; the bounds/choices/defaults single-sourcing _clamped's
// docstring cares about lives in settingSpecsOrder, not in this
// assignment step.
func assignSettingField(out *AutoSwitchSettings, field string, value any) {
	switch field {
	case "threshold":
		out.Threshold = value.(float64)
	case "interval_seconds":
		out.IntervalSeconds = value.(float64)
	case "cooldown_seconds":
		out.CooldownSeconds = value.(float64)
	case "hysteresis_pct":
		out.HysteresisPct = value.(float64)
	case "strategy":
		out.Strategy = value.(string)
	case "include_api_key_accounts":
		out.IncludeAPIKeyAccounts = value.(bool)
	case "unhealthy_ticks":
		out.UnhealthyTicks = value.(int)
	case "model":
		s := value.(string)
		out.Model = &s
	}
}

// LoadSettings mirrors load_settings: reads and clamps the autoswitch
// section of settingsPath(backupRoot), degrading to full defaults on any
// missing/corrupt/wrong-shape file or section.
func LoadSettings(backupRoot string) AutoSwitchSettings {
	raw := readSettingsRaw(SettingsPath(backupRoot))
	section, ok := raw["autoswitch"].(map[string]any)
	if !ok {
		return DefaultAutoSwitchSettings()
	}
	return clamped(section)
}

// SaveSettings mirrors save_settings: writes every one of the 8 known
// keys from settings, preserving any other top-level section and an
// already-set schemaVersion untouched. Used only by callers that intend
// to persist a full settings object; the CLI's `config set`/`unset`
// (SetSetting/UnsetSetting) deliberately write only the one key being
// changed instead, so they never freeze every current default into the
// file.
func SaveSettings(backupRoot string, settings AutoSwitchSettings) error {
	path := SettingsPath(backupRoot)
	raw := readSettingsRaw(path)
	if _, ok := raw["schemaVersion"]; !ok {
		raw["schemaVersion"] = SettingsSchemaVersion
	}
	section, ok := raw["autoswitch"].(map[string]any)
	if !ok {
		section = map[string]any{}
	}
	for _, spec := range settingSpecsOrder {
		switch spec.Field {
		case "threshold":
			section[spec.JSONKey] = settings.Threshold
		case "interval_seconds":
			section[spec.JSONKey] = settings.IntervalSeconds
		case "cooldown_seconds":
			section[spec.JSONKey] = settings.CooldownSeconds
		case "hysteresis_pct":
			section[spec.JSONKey] = settings.HysteresisPct
		case "strategy":
			section[spec.JSONKey] = settings.Strategy
		case "include_api_key_accounts":
			section[spec.JSONKey] = settings.IncludeAPIKeyAccounts
		case "unhealthy_ticks":
			section[spec.JSONKey] = settings.UnhealthyTicks
		case "model":
			if settings.Model == nil {
				section[spec.JSONKey] = nil
			} else {
				section[spec.JSONKey] = *settings.Model
			}
		}
	}
	raw["autoswitch"] = section
	return atomicWriteJSON(path, raw)
}

// boolWords mirrors _BOOL_WORDS.
var boolWords = map[string]bool{
	"true": true, "1": true, "yes": true,
	"false": false, "0": false, "no": false,
}

// ParseSettingValue mirrors parse_setting_value: the strict string parser
// `cswap config set` uses. Returns an ErrConfig-wrapped error with an
// exact, user-facing message on any invalid input; never silently
// substitutes a default (unlike the load-time clamp).
func ParseSettingValue(spec SettingSpec, raw string) (any, error) {
	switch spec.Kind {
	case "bool":
		b, ok := boolWords[strings.ToLower(strings.TrimSpace(raw))]
		if !ok {
			return nil, fmt.Errorf("%s expects true or false (or 1/0, yes/no), got '%s': %w",
				spec.Dotted(), raw, ErrConfig)
		}
		return b, nil
	case "choice":
		if !containsString(spec.Choices, raw) {
			return nil, fmt.Errorf("%s must be one of: %s: %w",
				spec.Dotted(), strings.Join(spec.Choices, ", "), ErrConfig)
		}
		return raw, nil
	case "string":
		value := strings.TrimSpace(raw)
		if value == "" {
			return nil, fmt.Errorf("%s expects a non-empty value; use 'cswap config unset %s' to clear it: %w",
				spec.Dotted(), spec.Dotted(), ErrConfig)
		}
		return value, nil
	default: // "int" or "float"
		var value float64
		var err error
		noun := "a number"
		if spec.Kind == "int" {
			noun = "an integer"
			var i int64
			i, err = strconv.ParseInt(raw, 10, 64)
			value = float64(i)
		} else {
			value, err = strconv.ParseFloat(raw, 64)
		}
		if err != nil {
			return nil, fmt.Errorf("%s expects %s, got '%s': %w", spec.Dotted(), noun, raw, ErrConfig)
		}
		if value < spec.Lo || value > spec.Hi {
			return nil, fmt.Errorf("%s must be between %s and %s: %w",
				spec.Dotted(), FormatSettingValue(spec.Lo), FormatSettingValue(spec.Hi), ErrConfig)
		}
		if spec.Kind == "int" {
			return int(value), nil
		}
		return value, nil
	}
}

// FormatSettingValue mirrors format_setting_value: how a value renders in
// settings.json echoes and `cswap config` output. nil -> "(none)"; bool
// checked before numeric (bool is not a number in Go, so this ordering
// matters less than in Python, but is kept explicit for parity); an
// integral float drops its trailing ".0".
func FormatSettingValue(value any) string {
	switch v := value.(type) {
	case nil:
		return "(none)"
	case bool:
		if v {
			return "true"
		}
		return "false"
	case float64:
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	case int:
		return strconv.Itoa(v)
	case string:
		return v
	default:
		return fmt.Sprintf("%v", v)
	}
}

// readSettingsRawForWrite mirrors _read_raw_for_write: the strict reader
// SetSetting/UnsetSetting use. Unlike readSettingsRaw, a missing file is
// still `{}`, but any read error, invalid JSON, or non-object top level
// raises an ErrConfig-wrapped error instead of silently discarding the
// user's existing (if corrupt) file.
func readSettingsRawForWrite(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, nil
		}
		return nil, fmt.Errorf("could not read %s: %v: %w", path, err, ErrConfig)
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("%s is not valid JSON (%v); fix or delete it before changing settings: %w",
			path, err, ErrConfig)
	}
	return parsed, nil
}

// SetSetting mirrors set_setting: validates rawValue against dottedKey's
// spec, then writes only that one key (plus schemaVersion, only if
// absent), never the other 7. Deliberately does not call SaveSettings,
// which would freeze every current default into the file.
func SetSetting(backupRoot, dottedKey, rawValue string) (any, error) {
	spec, err := SettingSpecFor(dottedKey)
	if err != nil {
		return nil, err
	}
	value, err := ParseSettingValue(spec, rawValue)
	if err != nil {
		return nil, err
	}
	path := SettingsPath(backupRoot)
	raw, err := readSettingsRawForWrite(path)
	if err != nil {
		return nil, err
	}
	if _, ok := raw["schemaVersion"]; !ok {
		raw["schemaVersion"] = SettingsSchemaVersion
	}
	section, ok := raw[spec.Section].(map[string]any)
	if !ok {
		section = map[string]any{}
	}
	section[spec.JSONKey] = value
	raw[spec.Section] = section
	if err := atomicWriteJSON(path, raw); err != nil {
		return nil, err
	}
	return value, nil
}

// UnsetSetting mirrors unset_setting: removes one key, returning false
// with no write at all if it wasn't present. Removes the whole section
// too if it becomes empty (cleans up a stray "autoswitch": {}).
func UnsetSetting(backupRoot, dottedKey string) (bool, error) {
	spec, err := SettingSpecFor(dottedKey)
	if err != nil {
		return false, err
	}
	path := SettingsPath(backupRoot)
	raw, err := readSettingsRawForWrite(path)
	if err != nil {
		return false, err
	}
	section, ok := raw[spec.Section].(map[string]any)
	if !ok {
		return false, nil
	}
	if _, present := section[spec.JSONKey]; !present {
		return false, nil
	}
	delete(section, spec.JSONKey)
	if _, ok := raw["schemaVersion"]; !ok {
		raw["schemaVersion"] = SettingsSchemaVersion
	}
	if len(section) == 0 {
		delete(raw, spec.Section)
	} else {
		raw[spec.Section] = section
	}
	if err := atomicWriteJSON(path, raw); err != nil {
		return false, err
	}
	return true, nil
}

// EffectiveSetting is one row of EffectiveSettings' result: a spec, its
// currently-effective (already-clamped) value, and whether it was
// explicitly present in settings.json (independent of whether that value
// happens to equal the default).
type EffectiveSetting struct {
	Spec  SettingSpec
	Value any
	IsSet bool
}

// EffectiveSettings mirrors effective_settings: one row per registered
// spec, in registry order, powering `cswap config list`/`get`.
func EffectiveSettings(backupRoot string) ([]EffectiveSetting, error) {
	raw := readSettingsRaw(SettingsPath(backupRoot))
	section, _ := raw["autoswitch"].(map[string]any)
	effective := LoadSettings(backupRoot)

	rows := make([]EffectiveSetting, len(settingSpecsOrder))
	for i, spec := range settingSpecsOrder {
		_, isSet := section[spec.JSONKey]
		var value any
		switch spec.Field {
		case "threshold":
			value = effective.Threshold
		case "interval_seconds":
			value = effective.IntervalSeconds
		case "cooldown_seconds":
			value = effective.CooldownSeconds
		case "hysteresis_pct":
			value = effective.HysteresisPct
		case "strategy":
			value = effective.Strategy
		case "include_api_key_accounts":
			value = effective.IncludeAPIKeyAccounts
		case "unhealthy_ticks":
			value = effective.UnhealthyTicks
		case "model":
			if effective.Model == nil {
				value = nil
			} else {
				value = *effective.Model
			}
		}
		rows[i] = EffectiveSetting{Spec: spec, Value: value, IsSet: isSet}
	}
	return rows, nil
}

// CliOverrides mirrors the 5 attrs merged_with_cli actually reads off
// argparse's `auto` namespace (threshold, interval, cooldown,
// include_api_key_accounts, model); strategy, unhealthy_ticks, and
// hysteresis_pct have no CLI override in Python and so have no field
// here either. A nil field means "not overridden", matching Python's
// `getattr(args, attr, None)` treating None as absent.
type CliOverrides struct {
	Threshold             *float64
	IntervalSeconds       *float64
	CooldownSeconds       *float64
	IncludeAPIKeyAccounts *bool
	Model                 *string
}

// MergedWithCli mirrors merged_with_cli: overlays only the non-nil
// overrides onto settings, then clamps, but only if at least one
// override was actually given (an all-nil CliOverrides returns settings
// unchanged, matching Python's no-clamp-if-nothing-changed short circuit).
func MergedWithCli(settings AutoSwitchSettings, overrides CliOverrides) AutoSwitchSettings {
	merged := settings
	changed := false
	if overrides.Threshold != nil {
		merged.Threshold = *overrides.Threshold
		changed = true
	}
	if overrides.IntervalSeconds != nil {
		merged.IntervalSeconds = *overrides.IntervalSeconds
		changed = true
	}
	if overrides.CooldownSeconds != nil {
		merged.CooldownSeconds = *overrides.CooldownSeconds
		changed = true
	}
	if overrides.IncludeAPIKeyAccounts != nil {
		merged.IncludeAPIKeyAccounts = *overrides.IncludeAPIKeyAccounts
		changed = true
	}
	if overrides.Model != nil {
		merged.Model = overrides.Model
		changed = true
	}
	if !changed {
		return settings
	}
	return clamped(settingsToRaw(merged))
}

// settingsToRaw converts an AutoSwitchSettings back into the same
// map[string]any shape clamped() reads from settings.json, so
// MergedWithCli's clamp step reuses the exact same per-field validation
// clamped() already implements instead of a second, separately
// maintained implementation that could drift from it (an earlier draft
// of this file had a hand-written clampSettings helper that only handled
// the 5 numeric fields, silently skipping strategy/model/
// includeApiKeyAccounts revalidation; that meant, for example, a
// --model "" CLI override would NOT correctly revert to Model=nil the
// way an empty string does when loaded from settings.json, since Python's
// _clamped treats an empty string as falsy for the string kind. Routing
// through clamped() for both call sites closes that gap by construction,
// not by remembering to keep two functions in sync).
func settingsToRaw(s AutoSwitchSettings) map[string]any {
	raw := map[string]any{
		"threshold":             s.Threshold,
		"intervalSeconds":       s.IntervalSeconds,
		"cooldownSeconds":       s.CooldownSeconds,
		"hysteresisPct":         s.HysteresisPct,
		"strategy":              s.Strategy,
		"includeApiKeyAccounts": s.IncludeAPIKeyAccounts,
		"unhealthyTicks":        float64(s.UnhealthyTicks), // clampedNumber expects float64, matching a JSON-decoded number
	}
	if s.Model != nil {
		raw["model"] = *s.Model
	}
	return raw
}
