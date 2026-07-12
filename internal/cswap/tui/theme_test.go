package tui

import "testing"

func TestSeverityColorBands(t *testing.T) {
	ok, warn, crit := 50.0, 75.0, 95.0
	tests := []struct {
		name string
		pct  *float64
		want lipglossColorWant
	}{
		{"nil is muted", nil, lipglossColorWant(Muted)},
		{"below warn band is ok", &ok, lipglossColorWant(SevOK)},
		{"at warn band", &warn, lipglossColorWant(SevWarn)},
		{"at crit band", &crit, lipglossColorWant(SevCrit)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SeverityColor(tt.pct); string(got) != string(tt.want) {
				t.Errorf("SeverityColor(%v) = %v, want %v", tt.pct, got, tt.want)
			}
		})
	}
}

type lipglossColorWant = string
