package cswap

import (
	"math"
	"math/rand"
)

// ServeTTLS mirrors SERVE_TTL_S: freshness floor shared by every
// collector. An entry younger than this is served from the store without
// any fetch, so the maximum sustained rate on one token is 1/ServeTTLS
// regardless of how many surfaces are open. In plain seconds, like every
// other constant in this file (NOT the nanoseconds convention the old
// usage_store.go-local ServeTTLS used; callers needing a time.Duration
// must multiply by time.Second explicitly at the point of use).
const ServeTTLS = 180.0

// MinIntervalS mirrors MIN_INTERVAL_S: normal cadence floor, movement can
// halve an interval down to this, never below.
const MinIntervalS = 180.0

// UrgentIntervalS mirrors URGENT_INTERVAL_S: the active account, within
// EscalationMarginPct of the switch threshold, with movement observed
// this poll (actually burning toward the limit).
const UrgentIntervalS = 60.0

// ActiveMaxIntervalS mirrors ACTIVE_MAX_INTERVAL_S: decay ceiling for the
// active account when its usage is not moving.
const ActiveMaxIntervalS = 300.0

// CandidateDefaultIntervalS mirrors CANDIDATE_DEFAULT_INTERVAL_S.
const CandidateDefaultIntervalS = 300.0

// CandidateMaxIntervalS mirrors CANDIDATE_MAX_INTERVAL_S: decay ceiling
// for an idle candidate.
const CandidateMaxIntervalS = 600.0

// MovementDeltaPct mirrors MOVEMENT_DELTA_PCT: a window whose binding pct
// moved at least this much between polls is being consumed somewhere
// (this machine, another PC, session mode).
const MovementDeltaPct = 1.0

// JitterFrac mirrors JITTER_FRAC: +/-fraction applied to each scheduled
// interval so independent processes drift apart instead of fetching in
// lockstep.
const JitterFrac = 0.1

// EdgeBackoffS mirrors EDGE_BACKOFF_S: reaction to a 429 with
// Retry-After: 0 (the saturated-window edge), probe at most this often.
const EdgeBackoffS = 300.0

// Post429MinIntervalS mirrors POST_429_MIN_INTERVAL_S: while any 429 was
// seen on the token within Recent429WindowS, floor the planned cadence
// here so freed capacity accumulates instead of being re-spent.
const Post429MinIntervalS = 360.0

// Recent429WindowS mirrors RECENT_429_WINDOW_S: matches the saturation
// horizon (a full trailing hour takes up to 60 minutes to age out).
const Recent429WindowS = 3600.0

// EscalationMarginPct mirrors ESCALATION_MARGIN_PCT: the engine escalates
// to a full candidate refresh when the active account is within this
// margin of the threshold; the urgent-mode cadence keys on the same band.
const EscalationMarginPct = 15.0

// ResetSlackS mirrors RESET_SLACK_S: never schedule a poll later than a
// known window reset (+ slack); stored usage is obsolete the moment the
// window rolls over.
const ResetSlackS = 60.0

// PlanAfterFetchInput mirrors plan_after_fetch's keyword-only parameters.
type PlanAfterFetchInput struct {
	PrevIntervalS *float64
	PrevUsage     map[string]any
	NewUsage      map[string]any
	IsActive      bool
	Threshold     float64
	Models        []string
	Recent429     bool
	Now           float64
	// Rng defaults to rand.Float64 when nil.
	Rng func() float64
}

// PlanAfterFetch mirrors plan_after_fetch: (nextPollAt, intervalS) for an
// account just fetched successfully.
//
// Movement (binding pct changed >= MovementDeltaPct since the previous
// poll) halves the interval, floored at MinIntervalS, or drops to
// UrgentIntervalS when the active account is moving inside the
// escalation band and not already at its limit (an at-limit account is
// about to skip straight to its reset regardless, so tightening the
// cadence first would waste budget). No movement backs off x1.5 toward
// the account's ceiling; unknown utilization uses the default. A recent
// 429 on this token floors the cadence at Post429MinIntervalS (and
// suppresses urgent mode) until Recent429WindowS has passed. The
// scheduled time gets JitterFrac noise, is never later than the
// account's next window reset (+ ResetSlackS), and an at-limit account
// skips straight to the reset that frees it (the learned interval is
// kept for its return).
func PlanAfterFetch(in PlanAfterFetchInput) (nextPollAt, intervalS float64) {
	rng := in.Rng
	if rng == nil {
		rng = rand.Float64
	}
	defaultInterval := CandidateDefaultIntervalS
	ceiling := CandidateMaxIntervalS
	if in.IsActive {
		defaultInterval = MinIntervalS
		ceiling = ActiveMaxIntervalS
	}
	base := defaultInterval
	if in.PrevIntervalS != nil && *in.PrevIntervalS != 0 {
		base = *in.PrevIntervalS
	}
	prevPct, prevOK := BindingPct(in.PrevUsage, in.Models)
	newPct, newOK := BindingPct(in.NewUsage, in.Models)
	var moving bool
	var interval float64
	if !prevOK || !newOK {
		moving = false
		interval = defaultInterval
	} else if math.Abs(newPct-prevPct) >= MovementDeltaPct {
		moving = true
		interval = max(MinIntervalS, base/2)
	} else {
		// Floored so a sub-floor base (urgent mode's 60s) snaps straight
		// back to the normal cadence once movement stops, instead of
		// decaying through 90s/135s polls the budget never intended.
		moving = false
		interval = min(ceiling, max(MinIntervalS, base*1.5))
	}
	// atLimit mirrors the same "no headroom left" check the reset-skip
	// logic below makes: an account already at its cap is about to jump
	// straight to its window reset regardless of cadence, so urgent
	// mode (meant to tighten polling while a switch is still avoidable)
	// would just burn budget for no benefit; the learned interval is
	// preserved instead, for the account's return after the reset.
	headroom, headroomOK := AccountHeadroom(in.NewUsage, in.Models)
	atLimit := headroomOK && headroom <= 0
	if in.IsActive && moving && !in.Recent429 && !atLimit && newOK && newPct >= in.Threshold-EscalationMarginPct {
		interval = UrgentIntervalS
	}
	if in.Recent429 {
		interval = max(interval, Post429MinIntervalS)
	}

	next := in.Now + interval*(1.0+JitterFrac*(2.0*rng()-1.0))
	if atLimit {
		if resetTs, ok := limitingResetTs(in.NewUsage, in.Models); ok && resetTs > next {
			next = resetTs
		}
	} else {
		if resetTs, ok := earliestFutureResetTs(in.NewUsage, in.Now, in.Models); ok {
			next = min(next, resetTs+ResetSlackS)
		}
	}
	return next, interval
}
