package cswap

import (
	"testing"
)

func fixedRng(v float64) func() float64 {
	return func() float64 { return v }
}

func TestPlanAfterFetchNoMovementDecaysTowardCeiling(t *testing.T) {
	prevInterval := 200.0
	next, interval := PlanAfterFetch(PlanAfterFetchInput{
		PrevIntervalS: &prevInterval,
		PrevUsage:     map[string]any{"five_hour": map[string]any{"pct": 50.0}},
		NewUsage:      map[string]any{"five_hour": map[string]any{"pct": 50.0}},
		IsActive:      true,
		Threshold:     90,
		Now:           1000,
		Rng:           fixedRng(0.5), // midpoint: zero jitter
	})
	if interval != 300.0 { // min(ActiveMaxIntervalS=300, max(MinIntervalS=180, 200*1.5=300))
		t.Fatalf("got interval %v", interval)
	}
	if next != 1000+300.0 {
		t.Fatalf("got next %v", next)
	}
}

func TestPlanAfterFetchMovementHalvesInterval(t *testing.T) {
	prevInterval := 300.0
	_, interval := PlanAfterFetch(PlanAfterFetchInput{
		PrevIntervalS: &prevInterval,
		PrevUsage:     map[string]any{"five_hour": map[string]any{"pct": 50.0}},
		NewUsage:      map[string]any{"five_hour": map[string]any{"pct": 55.0}},
		IsActive:      false,
		Threshold:     90,
		Now:           1000,
		Rng:           fixedRng(0.5),
	})
	if interval != 180.0 { // max(MinIntervalS=180, 300/2=150)
		t.Fatalf("got interval %v", interval)
	}
}

func TestPlanAfterFetchSubFloorBaseSnapsBackNotDecaysThrough(t *testing.T) {
	// A previous urgent-mode interval (60s) with no further movement must
	// snap straight to MinIntervalS (180), never a sub-floor 90s step.
	prevInterval := 60.0
	_, interval := PlanAfterFetch(PlanAfterFetchInput{
		PrevIntervalS: &prevInterval,
		PrevUsage:     map[string]any{"five_hour": map[string]any{"pct": 80.0}},
		NewUsage:      map[string]any{"five_hour": map[string]any{"pct": 80.0}},
		IsActive:      true,
		Threshold:     90,
		Now:           1000,
		Rng:           fixedRng(0.5),
	})
	if interval != 180.0 {
		t.Fatalf("got interval %v, expected a snap back to MinIntervalS", interval)
	}
}

func TestPlanAfterFetchUrgentModeOnActiveMovingNearThreshold(t *testing.T) {
	prevInterval := 300.0
	_, interval := PlanAfterFetch(PlanAfterFetchInput{
		PrevIntervalS: &prevInterval,
		PrevUsage:     map[string]any{"five_hour": map[string]any{"pct": 74.0}},
		NewUsage:      map[string]any{"five_hour": map[string]any{"pct": 76.0}}, // moved, and >= 90-15=75
		IsActive:      true,
		Threshold:     90,
		Now:           1000,
		Rng:           fixedRng(0.5),
	})
	if interval != UrgentIntervalS {
		t.Fatalf("got interval %v, expected urgent mode (%v)", interval, UrgentIntervalS)
	}
}

func TestPlanAfterFetchUrgentModeSuppressedByRecent429(t *testing.T) {
	prevInterval := 300.0
	_, interval := PlanAfterFetch(PlanAfterFetchInput{
		PrevIntervalS: &prevInterval,
		PrevUsage:     map[string]any{"five_hour": map[string]any{"pct": 74.0}},
		NewUsage:      map[string]any{"five_hour": map[string]any{"pct": 76.0}},
		IsActive:      true,
		Threshold:     90,
		Recent429:     true,
		Now:           1000,
		Rng:           fixedRng(0.5),
	})
	if interval != Post429MinIntervalS {
		t.Fatalf("got interval %v, expected the post-429 floor (%v), urgent mode must be suppressed", interval, Post429MinIntervalS)
	}
}

func TestPlanAfterFetchRecent429FloorsEvenNonUrgentInterval(t *testing.T) {
	// A tight interval that would otherwise result from movement (150) must
	// still be floored to Post429MinIntervalS (360) when recent429 is true.
	prevInterval := 300.0
	_, interval := PlanAfterFetch(PlanAfterFetchInput{
		PrevIntervalS: &prevInterval,
		PrevUsage:     map[string]any{"five_hour": map[string]any{"pct": 10.0}},
		NewUsage:      map[string]any{"five_hour": map[string]any{"pct": 20.0}}, // moved
		IsActive:      false,
		Threshold:     90,
		Recent429:     true,
		Now:           1000,
		Rng:           fixedRng(0.5),
	})
	if interval != Post429MinIntervalS {
		t.Fatalf("got interval %v, expected the post-429 floor (%v)", interval, Post429MinIntervalS)
	}
}

func TestPlanAfterFetchUnknownUsageUsesDefault(t *testing.T) {
	_, interval := PlanAfterFetch(PlanAfterFetchInput{
		PrevUsage: nil,
		NewUsage:  nil,
		IsActive:  true,
		Threshold: 90,
		Now:       1000,
		Rng:       fixedRng(0.5),
	})
	if interval != MinIntervalS {
		t.Fatalf("got interval %v, expected the active default (MinIntervalS)", interval)
	}
}

func TestPlanAfterFetchCandidateDefaultAndCeiling(t *testing.T) {
	_, interval := PlanAfterFetch(PlanAfterFetchInput{
		PrevUsage: nil,
		NewUsage:  nil,
		IsActive:  false,
		Threshold: 90,
		Now:       1000,
		Rng:       fixedRng(0.5),
	})
	if interval != CandidateDefaultIntervalS {
		t.Fatalf("got interval %v, expected the candidate default", interval)
	}
}

func TestPlanAfterFetchJitterAppliedToNextPollNotInterval(t *testing.T) {
	next, interval := PlanAfterFetch(PlanAfterFetchInput{
		PrevUsage: nil,
		NewUsage:  nil,
		IsActive:  true,
		Threshold: 90,
		Now:       1000,
		Rng:       fixedRng(1.0), // max jitter: +JitterFrac
	})
	if interval != MinIntervalS {
		t.Fatalf("got interval %v", interval)
	}
	wantNext := 1000 + MinIntervalS*(1.0+JitterFrac)
	if next != wantNext {
		t.Fatalf("got next %v, want %v", next, wantNext)
	}
}

func TestPlanAfterFetchAtLimitSkipsToLimitingReset(t *testing.T) {
	next, interval := PlanAfterFetch(PlanAfterFetchInput{
		PrevUsage: map[string]any{"five_hour": map[string]any{"pct": 90.0}},
		NewUsage: map[string]any{"five_hour": map[string]any{
			"pct":       100.0,
			"resets_at": "1970-01-01T00:30:00Z", // epoch 1800, well past next's normal target
		}},
		IsActive:  true,
		Threshold: 90,
		Now:       1000,
		Rng:       fixedRng(0.5),
	})
	if next != 1800 {
		t.Fatalf("got next %v, want the limiting reset epoch (1800)", next)
	}
	if interval != MinIntervalS {
		t.Fatalf("got interval %v, expected the learned interval kept even though next jumped to the reset", interval)
	}
}

func TestPlanAfterFetchNotAtLimitClampsToEarliestFutureReset(t *testing.T) {
	next, _ := PlanAfterFetch(PlanAfterFetchInput{
		PrevUsage: map[string]any{"five_hour": map[string]any{"pct": 50.0}},
		NewUsage: map[string]any{"five_hour": map[string]any{
			"pct":       50.0,
			"resets_at": "1970-01-01T00:17:10Z", // epoch 1030: ahead of now(1000), well before now+interval(1270)
		}},
		IsActive:  true,
		Threshold: 90,
		Now:       1000,
		Rng:       fixedRng(0.5),
	})
	want := 1030.0 + ResetSlackS
	if next != want {
		t.Fatalf("got next %v, want reset+slack (%v)", next, want)
	}
}
