// internal/cswap/tui/data_test.go
package tui

import (
	"errors"
	"testing"

	"github.com/mliem2k/claude-swap/internal/cswap"
)

func f64(v float64) *float64 { return &v }

func TestFormatAge(t *testing.T) {
	tests := []struct {
		name string
		ageS *float64
		want string
	}{
		{"nil is blank", nil, ""},
		{"fresh (below ServeTTLS) is blank", f64(1), ""},
		{"stale shows minutes", f64(150), "· 2m ago"},
		{"stale shows hours+minutes", f64(7980), "· 2h 13m ago"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatAge(tt.ageS); got != tt.want {
				t.Errorf("FormatAge(%v) = %q, want %q", tt.ageS, got, tt.want)
			}
		})
	}
}

func TestSentinelLabel(t *testing.T) {
	if got := SentinelLabel(cswap.UsageAPIKey); got != "API key (no quota)" {
		t.Errorf("SentinelLabel(UsageAPIKey) = %q", got)
	}
	if got := SentinelLabel("unknown-sentinel"); got != "unknown-sentinel" {
		t.Errorf("SentinelLabel fallback = %q, want the raw sentinel back", got)
	}
}

func TestRunActionSuccess(t *testing.T) {
	result := RunAction(func() (string, error) { return "added account 3", nil })
	if !result.OK || result.Message != "added account 3" {
		t.Errorf("got %+v", result)
	}
}

func TestRunActionFailure(t *testing.T) {
	result := RunAction(func() (string, error) { return "", errors.New("no active claude account found") })
	if result.OK || result.Message != "no active claude account found" {
		t.Errorf("got %+v", result)
	}
}

func TestRunSwitchActionSuccess(t *testing.T) {
	sr := &cswap.SwitchResult{Switched: true, Message: "Switched to Account-2 (b@example.com)"}
	result := RunSwitchAction(func() (*cswap.SwitchResult, error) { return sr, nil })
	if !result.OK || result.Switch == nil || !result.Switch.Switched {
		t.Errorf("got %+v", result)
	}
}
