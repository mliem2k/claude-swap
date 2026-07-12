package cswap

import (
	"errors"
	"testing"
)

func specFor(t *testing.T, dotted string) SettingSpec {
	t.Helper()
	spec, err := SettingSpecFor(dotted)
	if err != nil {
		t.Fatal(err)
	}
	return spec
}

func TestParseSettingValueFloatInRange(t *testing.T) {
	v, err := ParseSettingValue(specFor(t, "autoswitch.threshold"), "75.5")
	if err != nil {
		t.Fatal(err)
	}
	if v != 75.5 {
		t.Fatalf("got %#v", v)
	}
}

func TestParseSettingValueFloatOutOfRangeErrors(t *testing.T) {
	_, err := ParseSettingValue(specFor(t, "autoswitch.threshold"), "500")
	if !errors.Is(err, ErrConfig) {
		t.Fatalf("expected ErrConfig, got %v", err)
	}
}

func TestParseSettingValueIntNonNumericErrors(t *testing.T) {
	_, err := ParseSettingValue(specFor(t, "autoswitch.unhealthyTicks"), "not-a-number")
	if !errors.Is(err, ErrConfig) {
		t.Fatalf("expected ErrConfig, got %v", err)
	}
}

func TestParseSettingValueBoolWords(t *testing.T) {
	spec := specFor(t, "autoswitch.includeApiKeyAccounts")
	for raw, want := range map[string]bool{
		"true": true, "1": true, "yes": true, "TRUE": true,
		"false": false, "0": false, "no": false,
	} {
		v, err := ParseSettingValue(spec, raw)
		if err != nil {
			t.Fatalf("raw=%q: %v", raw, err)
		}
		if v != want {
			t.Fatalf("raw=%q got %#v want %v", raw, v, want)
		}
	}
}

func TestParseSettingValueBoolInvalidWordErrors(t *testing.T) {
	_, err := ParseSettingValue(specFor(t, "autoswitch.includeApiKeyAccounts"), "maybe")
	if !errors.Is(err, ErrConfig) {
		t.Fatalf("expected ErrConfig, got %v", err)
	}
}

func TestParseSettingValueChoiceValid(t *testing.T) {
	v, err := ParseSettingValue(specFor(t, "autoswitch.strategy"), "best")
	if err != nil {
		t.Fatal(err)
	}
	if v != "best" {
		t.Fatalf("got %#v", v)
	}
}

func TestParseSettingValueChoiceInvalidErrors(t *testing.T) {
	_, err := ParseSettingValue(specFor(t, "autoswitch.strategy"), "worst")
	if !errors.Is(err, ErrConfig) {
		t.Fatalf("expected ErrConfig, got %v", err)
	}
}

func TestParseSettingValueStringNonEmpty(t *testing.T) {
	v, err := ParseSettingValue(specFor(t, "autoswitch.model"), "Opus,Fable")
	if err != nil {
		t.Fatal(err)
	}
	if v != "Opus,Fable" {
		t.Fatalf("got %#v", v)
	}
}

func TestParseSettingValueStringEmptyErrors(t *testing.T) {
	_, err := ParseSettingValue(specFor(t, "autoswitch.model"), "   ")
	if !errors.Is(err, ErrConfig) {
		t.Fatalf("expected ErrConfig, got %v", err)
	}
	if err != nil && !contains(err.Error(), "cswap config unset") {
		t.Fatalf("expected the unset-hint text, got %q", err.Error())
	}
}

func TestFormatSettingValueVariants(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{nil, "(none)"},
		{true, "true"},
		{false, "false"},
		{90.0, "90"},
		{75.5, "75.5"},
		{"best", "best"},
		{3, "3"},
	}
	for _, c := range cases {
		if got := FormatSettingValue(c.in); got != c.want {
			t.Errorf("FormatSettingValue(%#v) = %q, want %q", c.in, got, c.want)
		}
	}
}
