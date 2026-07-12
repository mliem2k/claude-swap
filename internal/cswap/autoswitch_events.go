package cswap

import (
	"fmt"
	"math"
	"strings"
)

// TickOutcome mirrors TickOutcome: the outcome of one evaluation tick; the
// values double as `cswap auto --once` exit codes.
type TickOutcome int

const (
	TickSwitched TickOutcome = 0
	TickError    TickOutcome = 1
	TickNoAction TickOutcome = 2
	TickBlocked  TickOutcome = 3 // wanted to switch but no viable target / all exhausted
)

// WindowPct mirrors one entry of _window_pcts' ordered label -> pct
// mapping. Kept as an ordered slice (not a map) because window order ("5h"
// before "7d" before scoped model names) is meaningful in PollEvent's
// human-readable "5h 90% · 7d 45%" display, and Go map iteration order is
// not guaranteed.
type WindowPct struct {
	Label string
	Pct   float64
}

// AutoSwitchEvent mirrors AutoSwitchEvent: the base event contract.
// ToJSON payloads are additive: consumers must ignore unknown "event"
// kinds and unknown fields.
type AutoSwitchEvent interface {
	Kind() string
	Timestamp() string
	ToJSON() map[string]any
	Human() string
}

func eventJSON(kind, ts string, fields map[string]any) map[string]any {
	out := map[string]any{
		"schemaVersion": JSONSchemaVersion,
		"event":         kind,
		"ts":            ts,
	}
	for k, v := range fields {
		out[k] = v
	}
	return out
}

func refJSON(ref *AccountRef) any {
	if ref == nil {
		return nil
	}
	return AccountRefDict(ref.Number, ref.Email)
}

// PollEvent mirrors PollEvent: one usage poll's outcome, active account
// plus every candidate's headroom.
type PollEvent struct {
	ts string

	// numbers is the ordered set of account numbers this poll covers
	// (rotation order), used to keep Human()'s "others" list and JSON
	// iteration deterministic. Not present in Python (whose dict is
	// already insertion-ordered); see this plan's Global Constraints.
	numbers []string
	active  *AccountRef
	// headroom maps account number -> headroom pct, nil meaning unknown
	// (mirrors Python's float | None values).
	headroom    map[string]*float64
	threshold   float64
	fetchErrors map[string]string
	windows     map[string][]WindowPct
}

// NewPollEvent constructs a PollEvent with the current timestamp.
// numbers is the ordered account-number list headroom/fetchErrors/windows
// are keyed by; fetchErrors and windows may be nil (Python's default {}).
func NewPollEvent(
	numbers []string,
	active *AccountRef,
	headroom map[string]*float64,
	threshold float64,
	fetchErrors map[string]string,
	windows map[string][]WindowPct,
) *PollEvent {
	return &PollEvent{
		ts:          GetTimestamp(),
		numbers:     numbers,
		active:      active,
		headroom:    headroom,
		threshold:   threshold,
		fetchErrors: fetchErrors,
		windows:     windows,
	}
}

// Kind returns "poll".
func (e *PollEvent) Kind() string { return "poll" }

// Timestamp returns the event's ts field.
func (e *PollEvent) Timestamp() string { return e.ts }

// ToJSON mirrors PollEvent's to_json/_fields.
func (e *PollEvent) ToJSON() map[string]any {
	fields := map[string]any{
		"active":      refJSON(e.active),
		"headroomPct": e.headroom,
		"threshold":   e.threshold,
	}
	if len(e.fetchErrors) > 0 {
		fields["fetchErrors"] = e.fetchErrors
	}
	if len(e.windows) > 0 {
		// windowsPct must marshal as a label-keyed object per account
		// number (e.g. {"1": {"5h": 90.0, "7d": 45.0}}), matching Python's
		// dict[str, dict[str, float]]. e.windows itself stays an ordered
		// []WindowPct slice (see WindowPct's doc comment); only this JSON
		// payload is converted to the unordered label->pct shape.
		windowsJSON := make(map[string]map[string]float64, len(e.windows))
		for num, wins := range e.windows {
			inner := make(map[string]float64, len(wins))
			for _, w := range wins {
				inner[w.Label] = w.Pct
			}
			windowsJSON[num] = inner
		}
		fields["windowsPct"] = windowsJSON
	}
	return eventJSON(e.Kind(), e.ts, fields)
}

func (e *PollEvent) describe(num string) string {
	if wins := e.windows[num]; len(wins) > 0 {
		parts := make([]string, len(wins))
		for i, w := range wins {
			parts[i] = fmt.Sprintf("%s %.0f%%", w.Label, w.Pct)
		}
		return strings.Join(parts, " · ")
	}
	if h, ok := e.headroom[num]; ok && h != nil {
		return fmt.Sprintf("%.0f%%", 100-*h)
	}
	if err, ok := e.fetchErrors[num]; ok && err != "" {
		return fmt.Sprintf("? (%s)", err)
	}
	return "?"
}

// Human mirrors PollEvent.human.
func (e *PollEvent) Human() string {
	if e.active == nil {
		return "poll: no active account"
	}
	numStr := ""
	if e.active.Number != nil {
		numStr = fmt.Sprintf("%d", *e.active.Number)
	}
	var used string
	if h, ok := e.headroom[numStr]; ok && h != nil {
		used = fmt.Sprintf("%.0f%% used", 100-*h)
	} else if err, ok := e.fetchErrors[numStr]; ok && err != "" {
		used = fmt.Sprintf("usage unknown (%s)", err)
	} else {
		used = "usage unknown"
	}
	var others []string
	for _, n := range e.numbers {
		if n == numStr {
			continue
		}
		others = append(others, fmt.Sprintf("#%s: %s", n, e.describe(n)))
	}
	tail := ""
	if len(others) > 0 {
		tail = " | others: " + strings.Join(others, ", ")
	}
	return fmt.Sprintf("Account-%s (%s): %s (switch at %s%%)%s",
		numStr, e.active.Email, used, pctLabel(e.threshold), tail)
}

// SwitchEvent mirrors SwitchEvent: a completed (or, in dry-run, simulated)
// switch.
type SwitchEvent struct {
	ts       string
	trigger  string // "proactive" | "at-limit" | "failover"
	fromRef  *AccountRef
	toRef    *AccountRef
	warnings []string
	dryRun   bool
}

// NewSwitchEvent constructs a SwitchEvent with the current timestamp.
// warnings nil is normalized to an empty (not nil) slice, matching
// Python's default_factory=list.
func NewSwitchEvent(trigger string, fromRef, toRef *AccountRef, warnings []string, dryRun bool) *SwitchEvent {
	if warnings == nil {
		warnings = []string{}
	}
	return &SwitchEvent{
		ts: GetTimestamp(), trigger: trigger, fromRef: fromRef, toRef: toRef,
		warnings: warnings, dryRun: dryRun,
	}
}

// Kind returns "switch".
func (e *SwitchEvent) Kind() string { return "switch" }

// Timestamp returns the event's ts field.
func (e *SwitchEvent) Timestamp() string { return e.ts }

// ToJSON mirrors SwitchEvent's to_json/_fields.
func (e *SwitchEvent) ToJSON() map[string]any {
	return eventJSON(e.Kind(), e.ts, map[string]any{
		"trigger":  e.trigger,
		"from":     refJSON(e.fromRef),
		"to":       refJSON(e.toRef),
		"warnings": e.warnings,
		"dryRun":   e.dryRun,
	})
}

// Human mirrors SwitchEvent.human.
func (e *SwitchEvent) Human() string {
	src := "(none)"
	if e.fromRef != nil && e.fromRef.Number != nil {
		src = fmt.Sprintf("Account-%d", *e.fromRef.Number)
	}
	dst := "?"
	if e.toRef != nil && e.toRef.Number != nil {
		dst = fmt.Sprintf("Account-%d (%s)", *e.toRef.Number, e.toRef.Email)
	}
	prefix := "Switched"
	if e.dryRun {
		prefix = "[dry-run] would switch"
	}
	return fmt.Sprintf("%s %s -> %s (%s)", prefix, src, dst, e.trigger)
}

// NoSwitchEvent mirrors NoSwitchEvent: an evaluated-but-declined tick.
type NoSwitchEvent struct {
	ts     string
	reason string
	detail string
}

// NewNoSwitchEvent constructs a NoSwitchEvent with the current timestamp.
func NewNoSwitchEvent(reason, detail string) *NoSwitchEvent {
	return &NoSwitchEvent{ts: GetTimestamp(), reason: reason, detail: detail}
}

// Kind returns "no-switch".
func (e *NoSwitchEvent) Kind() string { return "no-switch" }

// Timestamp returns the event's ts field.
func (e *NoSwitchEvent) Timestamp() string { return e.ts }

// ToJSON mirrors NoSwitchEvent's to_json/_fields.
func (e *NoSwitchEvent) ToJSON() map[string]any {
	return eventJSON(e.Kind(), e.ts, map[string]any{"reason": e.reason, "detail": e.detail})
}

// Human mirrors NoSwitchEvent.human.
func (e *NoSwitchEvent) Human() string {
	if e.detail == "" {
		return "no switch: " + e.reason
	}
	return fmt.Sprintf("no switch: %s (%s)", e.reason, e.detail)
}

// QuarantineEvent mirrors QuarantineEvent: an account taken out of
// rotation.
type QuarantineEvent struct {
	ts     string
	number string
	email  string
	reason string
}

// NewQuarantineEvent constructs a QuarantineEvent with the current
// timestamp.
func NewQuarantineEvent(number, email, reason string) *QuarantineEvent {
	return &QuarantineEvent{ts: GetTimestamp(), number: number, email: email, reason: reason}
}

// Kind returns "account-quarantined".
func (e *QuarantineEvent) Kind() string { return "account-quarantined" }

// Timestamp returns the event's ts field.
func (e *QuarantineEvent) Timestamp() string { return e.ts }

// ToJSON mirrors QuarantineEvent's to_json/_fields.
func (e *QuarantineEvent) ToJSON() map[string]any {
	return eventJSON(e.Kind(), e.ts, map[string]any{
		"number": e.number, "email": e.email, "reason": e.reason,
	})
}

// Human mirrors QuarantineEvent.human.
func (e *QuarantineEvent) Human() string {
	return fmt.Sprintf(
		"Account-%s (%s) quarantined: %s. Log in with it and run 'cswap --add-account --slot %s' to recover.",
		e.number, e.email, e.reason, e.number,
	)
}

// UnquarantineEvent mirrors UnquarantineEvent: an account back in
// rotation.
type UnquarantineEvent struct {
	ts     string
	number string
	email  string
	reason string
}

// NewUnquarantineEvent constructs an UnquarantineEvent with the current
// timestamp. An empty reason defaults to "credentials-replaced", matching
// Python's dataclass field default.
func NewUnquarantineEvent(number, email, reason string) *UnquarantineEvent {
	if reason == "" {
		reason = "credentials-replaced"
	}
	return &UnquarantineEvent{ts: GetTimestamp(), number: number, email: email, reason: reason}
}

// Kind returns "account-unquarantined".
func (e *UnquarantineEvent) Kind() string { return "account-unquarantined" }

// Timestamp returns the event's ts field.
func (e *UnquarantineEvent) Timestamp() string { return e.ts }

// ToJSON mirrors UnquarantineEvent's to_json/_fields.
func (e *UnquarantineEvent) ToJSON() map[string]any {
	return eventJSON(e.Kind(), e.ts, map[string]any{
		"number": e.number, "email": e.email, "reason": e.reason,
	})
}

// Human mirrors UnquarantineEvent.human.
func (e *UnquarantineEvent) Human() string {
	return fmt.Sprintf("Account-%s (%s) back in rotation (%s)", e.number, e.email, e.reason)
}

// AllExhaustedEvent mirrors AllExhaustedEvent: every account is at its
// limit.
type AllExhaustedEvent struct {
	ts              string
	earliestResetAt *string
}

// NewAllExhaustedEvent constructs an AllExhaustedEvent with the current
// timestamp.
func NewAllExhaustedEvent(earliestResetAt *string) *AllExhaustedEvent {
	return &AllExhaustedEvent{ts: GetTimestamp(), earliestResetAt: earliestResetAt}
}

// Kind returns "all-exhausted".
func (e *AllExhaustedEvent) Kind() string { return "all-exhausted" }

// Timestamp returns the event's ts field.
func (e *AllExhaustedEvent) Timestamp() string { return e.ts }

// ToJSON mirrors AllExhaustedEvent's to_json/_fields.
func (e *AllExhaustedEvent) ToJSON() map[string]any {
	var v any
	if e.earliestResetAt != nil {
		v = *e.earliestResetAt
	}
	return eventJSON(e.Kind(), e.ts, map[string]any{"earliestResetAt": v})
}

// Human mirrors AllExhaustedEvent.human.
func (e *AllExhaustedEvent) Human() string {
	if e.earliestResetAt != nil {
		return "all accounts exhausted; earliest reset " + *e.earliestResetAt
	}
	return "all accounts exhausted; no reset time known"
}

// SleepEvent mirrors SleepEvent: the engine is about to sleep until the
// next tick.
type SleepEvent struct {
	ts      string
	seconds float64
	until   string
}

// NewSleepEvent constructs a SleepEvent with the current timestamp.
func NewSleepEvent(seconds float64, until string) *SleepEvent {
	return &SleepEvent{ts: GetTimestamp(), seconds: seconds, until: until}
}

// Kind returns "sleep".
func (e *SleepEvent) Kind() string { return "sleep" }

// Timestamp returns the event's ts field.
func (e *SleepEvent) Timestamp() string { return e.ts }

// ToJSON mirrors SleepEvent's to_json/_fields.
func (e *SleepEvent) ToJSON() map[string]any {
	return eventJSON(e.Kind(), e.ts, map[string]any{
		"seconds": math.Round(e.seconds*10) / 10,
		"until":   e.until,
	})
}

// Human mirrors SleepEvent.human.
func (e *SleepEvent) Human() string {
	return fmt.Sprintf("sleeping %.0fm (until %s)", e.seconds/60, e.until)
}

// ErrorEvent mirrors ErrorEvent: a tick failed; transient means the
// engine will retry.
type ErrorEvent struct {
	ts        string
	message   string
	transient bool
}

// NewErrorEvent constructs an ErrorEvent with the current timestamp.
func NewErrorEvent(message string, transient bool) *ErrorEvent {
	return &ErrorEvent{ts: GetTimestamp(), message: message, transient: transient}
}

// Kind returns "error".
func (e *ErrorEvent) Kind() string { return "error" }

// Timestamp returns the event's ts field.
func (e *ErrorEvent) Timestamp() string { return e.ts }

// ToJSON mirrors ErrorEvent's to_json/_fields.
func (e *ErrorEvent) ToJSON() map[string]any {
	return eventJSON(e.Kind(), e.ts, map[string]any{
		"message": e.message, "transient": e.transient,
	})
}

// Human mirrors ErrorEvent.human.
func (e *ErrorEvent) Human() string {
	if e.transient {
		return "error: " + e.message + " (will retry)"
	}
	return "error: " + e.message
}

// ConfigWarningEvent mirrors ConfigWarningEvent: a configuration value is
// syntactically fine but provably inert (e.g. an autoswitch.model name no
// account reports). Not an error: the engine keeps running on the axes
// that do exist.
type ConfigWarningEvent struct {
	ts      string
	message string
}

// NewConfigWarningEvent constructs a ConfigWarningEvent with the current
// timestamp.
func NewConfigWarningEvent(message string) *ConfigWarningEvent {
	return &ConfigWarningEvent{ts: GetTimestamp(), message: message}
}

// Kind returns "config-warning".
func (e *ConfigWarningEvent) Kind() string { return "config-warning" }

// Timestamp returns the event's ts field.
func (e *ConfigWarningEvent) Timestamp() string { return e.ts }

// ToJSON mirrors ConfigWarningEvent's to_json/_fields.
func (e *ConfigWarningEvent) ToJSON() map[string]any {
	return eventJSON(e.Kind(), e.ts, map[string]any{"message": e.message})
}

// Human mirrors ConfigWarningEvent.human.
func (e *ConfigWarningEvent) Human() string {
	return "warning: " + e.message
}
