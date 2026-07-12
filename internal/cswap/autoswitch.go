package cswap

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// StateFilename mirrors STATE_FILENAME: the autoswitch state file's name
// under a switcher's backup directory.
const StateFilename = "autoswitch_state.json"

// StateSchemaVersion mirrors STATE_SCHEMA_VERSION: the autoswitch state
// file's own schema version, distinct from JSONSchemaVersion (the CLI
// output schema).
const StateSchemaVersion = 1

// pctLabel mirrors pct_label: a percentage for display, as configured.
// 85.555555 stays itself (never a rounded "85.5556"), and 99.9 never
// becomes a lying "100" the way "%.0f" renders it. Ten significant
// digits still absorb IEEE float noise (~15th digit) in computed
// utilizations (100.0 - headroom). Displayed comparisons must format
// BOTH sides with this helper, mixing formatters can render an
// impossible "85.5556% < 85.555555%".
func pctLabel(value float64) string {
	return strconv.FormatFloat(value, 'g', 10, 64)
}

// AutoSwitchEngine mirrors AutoSwitchEngine: a threshold-policy
// auto-switcher over a ClaudeAccountSwitcher. OnEvent receives every
// AutoSwitchEvent. Clock is wall time (persisted cooldown timestamps
// must survive processes).
type AutoSwitchEngine struct {
	Switcher  *ClaudeAccountSwitcher
	Settings  AutoSwitchSettings
	OnEvent   func(AutoSwitchEvent)
	DryRun    bool
	StatePath string
	Clock     func() time.Time

	// models is settings.Model, comma-split and parsed once at
	// construction (mirrors Python's self._models), so every usage-window
	// read (decisions, cadence, reset scheduling) sees the same axes.
	models []string

	unhealthyTicks int

	// sleepUntilTs and blockedWaitLong are set per tick by tickInner: a
	// known-reset sleep target, and whether a BLOCKED outcome is static
	// enough to wait longer than normal.
	sleepUntilTs    *float64
	blockedWaitLong bool

	// idleHoldSince survives across ticks (elapsed-time cap); idleHoldSlow
	// is per-tick. Both set by tickInner.
	idleHoldSince *float64
	idleHoldSlow  bool

	// modelCheckDone is a one-shot typo guard for autoswitch.model,
	// resolved on the first tick where every relevant account has
	// readable usage. True immediately when no model filter is
	// configured (mirrors Python's `not self._models`).
	modelCheckDone bool

	// stopMu guards recreating/closing stopCh so Stop (often called from a
	// signal handler goroutine) and RunLoop (which recreates stopCh on
	// every call, mirroring Python's self._stop.clear()) never race.
	stopMu sync.Mutex
	// stopCh is closed by Stop to wake RunLoop from any sleep.
	stopCh chan struct{}

	// settingsMu guards Settings against ApplyThreshold racing RunLoop's
	// own goroutine reading it via currentSettings. Python relies on the
	// GIL making a single attribute reassignment atomic; Go has no such
	// guarantee, so this is explicit here.
	settingsMu sync.RWMutex

	// wakeCh cuts RunLoop's current inter-tick sleep short without asking
	// it to exit (unlike stopCh). Buffered size 1: Wake's send is
	// non-blocking, and a wake requested at any point before RunLoop's
	// next select is never lost — receiving from the channel is the
	// "clear", there is no separate clear step needed.
	wakeCh chan struct{}
}

// NewAutoSwitchEngine mirrors AutoSwitchEngine.__init__. statePath "" and
// clock nil take Python's defaults (switcher.BackupDir/StateFilename and
// time.Now).
func NewAutoSwitchEngine(
	switcher *ClaudeAccountSwitcher,
	settings AutoSwitchSettings,
	onEvent func(AutoSwitchEvent),
	dryRun bool,
	statePath string,
	clock func() time.Time,
) *AutoSwitchEngine {
	if statePath == "" {
		statePath = filepath.Join(switcher.BackupDir, StateFilename)
	}
	if clock == nil {
		clock = time.Now
	}
	modelStr := ""
	if settings.Model != nil {
		modelStr = *settings.Model
	}
	models := ParseModelNames(modelStr)
	// Poll plans written by the collector must key on the same threshold/
	// models the engine decides with (CLI overrides included), not on
	// whatever the settings file happens to say.
	switcher.SetPollPolicyInputs(settings.Threshold, models)
	return &AutoSwitchEngine{
		Switcher:       switcher,
		Settings:       settings,
		OnEvent:        onEvent,
		DryRun:         dryRun,
		StatePath:      statePath,
		Clock:          clock,
		models:         models,
		modelCheckDone: len(models) == 0,
		stopCh:         make(chan struct{}),
	}
}

// stateLockPath mirrors _state_lock's lock path: a dedicated lock file
// alongside the state file, distinct from switcher.LockFile.
func (e *AutoSwitchEngine) stateLockPath() string {
	return filepath.Join(filepath.Dir(e.StatePath), ".autoswitch_state.lock")
}

// readState mirrors _read_state: the state file's parsed contents, or an
// empty map on any read/parse failure or non-object JSON.
func (e *AutoSwitchEngine) readState() map[string]any {
	data, err := os.ReadFile(e.StatePath)
	if err != nil {
		return map[string]any{}
	}
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return map[string]any{}
	}
	m, ok := raw.(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return m
}

// mutateState mirrors _mutate_state: read-modify-write the state file
// under its lock; returns the new state. The lock prevents two concurrent
// engines (loop + cron --once) from overwriting each other's
// quarantine/cooldown updates. Never call while any other lock is held.
// Returns an error instead of Python's implicit exception propagation
// (caught by tick()'s outer try/except in a future plan); callers must
// handle it explicitly.
func (e *AutoSwitchEngine) mutateState(mutator func(map[string]any)) (map[string]any, error) {
	lock := NewFileLock(e.stateLockPath(), 10*time.Second)
	if err := lock.Lock(10 * time.Second); err != nil {
		return nil, err
	}
	defer lock.Release()

	state := e.readState()
	state["schemaVersion"] = StateSchemaVersion
	mutator(state)
	if err := atomicWriteJSON(e.StatePath, state); err != nil {
		return nil, err
	}
	return state, nil
}

// Emit mirrors _emit: hands an event to OnEvent.
func (e *AutoSwitchEngine) Emit(event AutoSwitchEvent) {
	e.OnEvent(event)
}

// currentSettings is a concurrency-safe snapshot of Settings, for every
// read site outside of construction. tickInner already snapshots once per
// tick into a local var; inCooldown and nextDelay/RunLoop call this
// directly since, although they run on RunLoop's own goroutine, they must
// still synchronize against ApplyThreshold running on a different one
// (typically the TUI's).
func (e *AutoSwitchEngine) currentSettings() AutoSwitchSettings {
	e.settingsMu.RLock()
	defer e.settingsMu.RUnlock()
	return e.Settings
}

// ApplyThreshold mirrors apply_threshold: a session override from the
// TUI, retargeting the trigger and poll cadence mid-run. Threshold only,
// the model axes are fixed at construction. Safe to call from any
// goroutine while RunLoop is running.
func (e *AutoSwitchEngine) ApplyThreshold(threshold float64) {
	e.settingsMu.Lock()
	e.Settings.Threshold = threshold
	e.settingsMu.Unlock()
	e.Switcher.SetPollPolicyInputs(threshold, e.models)
}
