// internal/cswap/tui/autoview.go
package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mliem2k/claude-swap/internal/cswap"
)

// engineEventMsg carries one AutoSwitchEvent off the engine's goroutine.
type engineEventMsg struct{ event cswap.AutoSwitchEvent }

// eventStyles mirrors _EVENT_STYLES: severity color per event kind, keyed
// by AutoSwitchEvent.Kind(). Kinds absent here fall through to
// quietEventKinds, then to the plain foreground.
var eventStyles = map[string]lipgloss.Color{
	"switch":              Accent,
	"error":               SevWarn,
	"account-quarantined": SevWarn,
	"all-exhausted":       SevCrit,
}

// quietEventKinds mirrors _QUIET_KINDS: routine, low-signal events dim to
// muted instead of the plain foreground.
var quietEventKinds = map[string]bool{
	"poll": true, "no-switch": true, "sleep": true, "account-unquarantined": true,
}

// eventColor picks the severity color for one event kind: an explicit
// eventStyles entry, else Muted for a quietEventKinds member, else the
// plain foreground. Split out from eventText so the kind-to-color mapping
// itself is unit-testable without depending on lipgloss's tty detection
// (Render strips ANSI codes entirely in a non-tty test environment).
func eventColor(kind string) lipgloss.Color {
	if color, ok := eventStyles[kind]; ok {
		return color
	}
	if quietEventKinds[kind] {
		return Muted
	}
	return Foreground
}

// eventText mirrors event_text: one styled log line for an engine event,
// colored by kind so switches/errors/quarantines/exhaustion stand out from
// routine poll noise, matching the CLI's own human-readable severity cues.
func eventText(event cswap.AutoSwitchEvent) string {
	stamp := StyleMuted.Render(ClockStamp() + "  ")
	line := lipgloss.NewStyle().Foreground(eventColor(event.Kind())).Render(event.Human())
	return stamp + line
}

// autoScreen mirrors AutoScreen: the real engine, visualized. Opens in
// dry-run; going live is an explicit, confirmed action.
type autoScreen struct {
	app    *App
	engine *cswap.AutoSwitchEngine
	events chan cswap.AutoSwitchEvent
	log    []string
	dryRun bool

	settings            cswap.AutoSwitchSettings
	configuredThreshold float64
	entryThreshold      float64
	adjusting           bool

	models []string
}

func newAutoScreen(app *App) screen {
	return &autoScreen{app: app, dryRun: true}
}

func (s *autoScreen) Init() tea.Cmd {
	s.app.SetStoreOnly(true)
	s.settings = cswap.LoadSettings(s.app.Switcher.BackupDir)
	s.configuredThreshold = s.settings.Threshold
	s.app.ThresholdPct = &s.settings.Threshold
	modelStr := ""
	if s.settings.Model != nil {
		modelStr = *s.settings.Model
	}
	s.models = cswap.ParseModelNames(modelStr)
	return s.startEngine(true)
}

func (s *autoScreen) startEngine(dryRun bool) tea.Cmd {
	s.dryRun = dryRun
	// Capture the channel in a local so this engine's OnEvent closure sends
	// to the channel it was born with, never the s.events field live. Stop()
	// only signals (it does not join), so a restarted engine's goroutine can
	// still Emit after restartEngine reassigns s.events; reading the field at
	// send time would both data-race that reassignment and misroute the late
	// event onto the new engine's channel. readEventCmd already snapshots the
	// field the same way; this keeps the two symmetric.
	events := make(chan cswap.AutoSwitchEvent, 32)
	s.events = events
	s.engine = cswap.NewAutoSwitchEngine(s.app.Switcher, s.settings, func(e cswap.AutoSwitchEvent) {
		events <- e
	}, dryRun, "", nil)
	mode := "DRY-RUN (watching only)"
	if !dryRun {
		mode = "LIVE (will switch accounts)"
	}
	s.log = append(s.log, "— engine started: "+mode+" —")
	engine := s.engine
	return tea.Batch(
		func() tea.Msg {
			go func() {
				engine.RunLoop()
				// RunLoop only returns once it has actually observed Stop()
				// (or the process is exiting), and OnEvent above is the only
				// sender events ever has, always from this same goroutine, so
				// closing here can never race a send. This is what unblocks
				// the readEventCmd goroutine restartEngine leaves parked on
				// the old channel when a live/dry-run toggle swaps s.events
				// out from under it: readEventCmd's `e, ok := <-events`
				// already treats a closed channel as "done, stop rescheduling"
				// (ok is false), so no reader-side change is needed either.
				close(events)
			}()
			return nil
		},
		s.readEventCmd(),
	)
}

func (s *autoScreen) readEventCmd() tea.Cmd {
	events := s.events
	return func() tea.Msg {
		e, ok := <-events
		if !ok {
			return nil
		}
		return engineEventMsg{event: e}
	}
}

func (s *autoScreen) restartEngine(dryRun bool) tea.Cmd {
	if s.engine != nil {
		s.engine.Stop()
	}
	return s.startEngine(dryRun)
}

func (s *autoScreen) onUnmount() {
	if s.engine != nil {
		s.engine.Stop()
	}
	s.app.Switcher.ClearPollPolicyInputs()
	s.app.ThresholdPct = &s.configuredThreshold
	s.app.SetStoreOnly(false)
}

func (s *autoScreen) startAdjust() {
	s.adjusting = true
	s.entryThreshold = s.settings.Threshold
}

func (s *autoScreen) endAdjust() tea.Cmd {
	s.adjusting = false
	if s.settings.Threshold == s.entryThreshold {
		return nil
	}
	s.engine.Wake()
	s.log = append(s.log, fmt.Sprintf("— threshold set to %v%% for this session —", s.settings.Threshold))
	return nil
}

func (s *autoScreen) stepThreshold(delta float64) {
	if !s.adjusting {
		return
	}
	spec, err := cswap.SettingSpecFor("autoswitch.threshold")
	if err != nil {
		return
	}
	value := s.settings.Threshold + delta
	if value < spec.Lo {
		value = spec.Lo
	}
	if value > spec.Hi {
		value = spec.Hi
	}
	if value == s.settings.Threshold {
		return
	}
	s.settings.Threshold = value
	s.engine.ApplyThreshold(value)
	s.app.ThresholdPct = &s.settings.Threshold
}

func (s *autoScreen) Update(msg tea.Msg) (screen, tea.Cmd) {
	switch m := msg.(type) {
	case engineEventMsg:
		s.log = append(s.log, eventText(m.event))
		return s, s.readEventCmd()
	case tea.KeyMsg:
		return s.handleKey(m)
	}
	return s, nil
}

func (s *autoScreen) handleKey(m tea.KeyMsg) (screen, tea.Cmd) {
	switch m.String() {
	case "l":
		// Going live lets the engine actually switch the user's active account,
		// so it is never a silent flip: dry-run -> live must be an explicit,
		// confirmed action (mirrors autoview.py, "opening a view must never
		// start switching accounts on its own"). Going back to dry-run is always
		// safe and needs no gate.
		if s.dryRun {
			return s, func() tea.Msg {
				return pushScreenMsg{adaptScreen(NewConfirmModal(
					"Go live? claude-swap will switch your active account automatically when the threshold is reached.\n\n(Same behavior as running `cswap auto` in a terminal.)",
					"Go live", "Go live",
					func(confirmed bool) {
						if confirmed {
							// restartEngine returns the cmd that launches the live
							// engine's RunLoop goroutine and re-registers the event
							// reader; dropping it would flip the badge to LIVE but
							// never actually start switching. Thread it back through
							// app.go's pendingCmd, the exact mechanism its own
							// ConfirmRemove/ActionAddCurrent confirm flows use, so
							// App runs it once the modal is popped.
							s.app.pendingCmd = s.restartEngine(false)
						}
					},
				))}
			}
		}
		return s, s.restartEngine(true)
	case "t":
		if s.adjusting {
			return s, s.endAdjust()
		}
		s.startAdjust()
		return s, nil
	case "left":
		s.stepThreshold(-1)
		return s, nil
	case "right":
		s.stepThreshold(1)
		return s, nil
	case "enter":
		if s.adjusting {
			return s, s.endAdjust()
		}
		return s, nil
	case "esc", "q":
		if s.adjusting {
			return s, s.endAdjust()
		}
		s.onUnmount()
		return s, popScreen()
	}
	return s, nil
}

func (s *autoScreen) View() string {
	var b strings.Builder
	accounts := []cswap.AccountSnapshot(nil)
	if s.app.Snapshot != nil {
		accounts = s.app.Snapshot.Accounts
	}
	b.WriteString(AccountsPanelText(accounts, false, s.app.ThresholdPct, s.app.ContentWidth()))
	b.WriteString("\n\n")

	badge := " DRY-RUN "
	if !s.dryRun {
		badge = " LIVE "
	}
	b.WriteString(badge)
	b.WriteString("  auto-switch · threshold ")
	fmt.Fprintf(&b, "%v%%", s.settings.Threshold)
	if s.settings.Threshold != s.configuredThreshold {
		b.WriteString(" (session)")
	}
	fmt.Fprintf(&b, " · poll every %.0fs", s.settings.IntervalSeconds)
	if s.adjusting {
		b.WriteString("   ← → adjust · enter done")
	}
	b.WriteString("\n\n")

	b.WriteString(s.candidatesView())
	b.WriteString("\n\n")

	start := 0
	if len(s.log) > 12 {
		start = len(s.log) - 12
	}
	b.WriteString(strings.Join(s.log[start:], "\n"))
	b.WriteString("\n\n")
	b.WriteString(renderFooter("l go live/dry-run", "t threshold", "esc back"))
	return b.String()
}

func (s *autoScreen) candidatesView() string {
	var b strings.Builder
	b.WriteString("Next best")
	if s.app.Snapshot == nil {
		return b.String()
	}
	type ranked struct {
		pct    float64
		number string
		line   string
	}
	var rows []ranked
	for _, acc := range s.app.Snapshot.Accounts {
		if acc.IsActive || !acc.Switchable {
			continue
		}
		if acc.Usage.Sentinel != nil {
			rows = append(rows, ranked{998, acc.Number, fmt.Sprintf("\n  %2s  %s  %s", acc.Number, acc.Email, SentinelLabel(*acc.Usage.Sentinel))})
			continue
		}
		pct, ok := cswap.BindingPct(acc.Usage.LastGood, s.models)
		if !ok {
			rows = append(rows, ranked{999, acc.Number, fmt.Sprintf("\n  %2s  %s  usage unknown", acc.Number, acc.Email)})
			continue
		}
		rows = append(rows, ranked{pct, acc.Number, fmt.Sprintf("\n  %2s  %s  %3.0f%% used", acc.Number, acc.Email, pct)})
	}
	if len(rows) == 0 {
		b.WriteString("\n  no other switchable accounts")
		return b.String()
	}
	// Mirrors sorted(ranked)'s (pct, number) tuple sort: number is a
	// string tiebreaker on a full pct tie (e.g. two sentinel-state or
	// two usage-unknown accounts sharing the same 998/999 sort key), not
	// left at whatever order accounts happened to appear in the
	// snapshot the way a pct-only comparator would.
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].pct != rows[j].pct {
			return rows[i].pct < rows[j].pct
		}
		return rows[i].number < rows[j].number
	})
	for _, r := range rows {
		b.WriteString(r.line)
	}
	return b.String()
}
