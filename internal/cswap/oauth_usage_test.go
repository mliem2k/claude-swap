package cswap

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func withUsageURL(t *testing.T, url string) {
	t.Helper()
	old := usageAPIURL
	usageAPIURL = url
	t.Cleanup(func() { usageAPIURL = old })
}

func TestBuildUsageResultFiveHourAndSevenDay(t *testing.T) {
	future := "2099-01-01T00:00:00Z"
	raw := map[string]any{
		"five_hour": map[string]any{"utilization": 42.0, "resets_at": future},
		"seven_day": map[string]any{"utilization": 10.0, "resets_at": future},
	}
	got := BuildUsageResult(raw)
	h5 := got["five_hour"].(map[string]any)
	if h5["pct"] != 42.0 {
		t.Fatalf("got %#v", got)
	}
	d7 := got["seven_day"].(map[string]any)
	if d7["pct"] != 10.0 {
		t.Fatalf("got %#v", got)
	}
}

func TestBuildUsageResultScopedLimits(t *testing.T) {
	raw := map[string]any{
		"limits": []any{
			map[string]any{
				"scope":   map[string]any{"model": map[string]any{"display_name": "Fable"}},
				"percent": 55.0,
			},
		},
	}
	got := BuildUsageResult(raw)
	scoped := got["scoped"].([]map[string]any)
	if len(scoped) != 1 || scoped[0]["name"] != "Fable" || scoped[0]["pct"] != 55.0 {
		t.Fatalf("got %#v", got)
	}
}

func TestBuildUsageResultNilWhenEmpty(t *testing.T) {
	if got := BuildUsageResult(map[string]any{}); got != nil {
		t.Fatalf("expected nil for an empty response, got %#v", got)
	}
}

func TestRelevantWindowsIncludesNamedModel(t *testing.T) {
	usage := map[string]any{
		"five_hour": map[string]any{"pct": 20.0},
		"scoped":    []map[string]any{{"name": "Fable", "pct": 90.0}},
	}
	windows := RelevantWindows(usage, []string{"fable"})
	found := false
	for _, w := range windows {
		if w.Label == "Fable" && w.Pct == 90.0 {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected Fable window (case-insensitive match), got %#v", windows)
	}
}

// TestRelevantWindowsIncludesNamedModelAfterJSONRoundTrip is the
// regression test for a real bug: a bare `usage["scoped"].([]map[string]any)`
// assertion only ever matched a usage map built with that exact native Go
// type in memory (as every existing test, including
// TestRelevantWindowsIncludesNamedModel above, happens to construct it).
// Once usage has been through even one JSON round trip, exactly what
// UsageStore does for every account not freshly fetched this tick,
// encoding/json decodes the array as []any (map[string]any elements), and
// the old assertion silently returned nothing. This test builds usage the
// same way real store-served data arrives, via json.Marshal/Unmarshal, not
// a hand-built literal.
func TestRelevantWindowsIncludesNamedModelAfterJSONRoundTrip(t *testing.T) {
	original := map[string]any{
		"five_hour": map[string]any{"pct": 20.0},
		"scoped":    []map[string]any{{"name": "Fable", "pct": 90.0}},
	}
	body, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var usage map[string]any
	if err := json.Unmarshal(body, &usage); err != nil {
		t.Fatal(err)
	}
	if _, ok := usage["scoped"].([]map[string]any); ok {
		t.Fatal("test setup invariant broken: encoding/json should never decode a JSON array as []map[string]any directly")
	}

	windows := RelevantWindows(usage, []string{"fable"})
	found := false
	for _, w := range windows {
		if w.Label == "Fable" && w.Pct == 90.0 {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected Fable window after a JSON round trip (case-insensitive match), got %#v", windows)
	}
}

// TestScopedWindowsHandlesBothNativeAndJSONDecodedShapes covers
// ScopedWindows directly: both the native []map[string]any shape (a fresh
// oauth.go fetch) and the []any-of-map[string]any shape (JSON round trip).
func TestScopedWindowsHandlesBothNativeAndJSONDecodedShapes(t *testing.T) {
	native := map[string]any{"scoped": []map[string]any{{"name": "Fable", "pct": 90.0}}}
	got := ScopedWindows(native)
	if len(got) != 1 || got[0]["name"] != "Fable" {
		t.Fatalf("native shape: got %#v", got)
	}

	decoded := map[string]any{"scoped": []any{map[string]any{"name": "Fable", "pct": 90.0}}}
	got = ScopedWindows(decoded)
	if len(got) != 1 || got[0]["name"] != "Fable" {
		t.Fatalf("JSON-decoded shape: got %#v", got)
	}

	if got := ScopedWindows(map[string]any{}); got != nil {
		t.Fatalf("expected nil when scoped is absent, got %#v", got)
	}
}

func TestAccountHeadroomUnknownWithNoData(t *testing.T) {
	_, ok := AccountHeadroom(nil, nil)
	if ok {
		t.Fatal("expected unknown (ok=false) for nil usage")
	}
}

func TestAccountHeadroomBindingWindow(t *testing.T) {
	usage := map[string]any{
		"five_hour": map[string]any{"pct": 30.0},
		"seven_day": map[string]any{"pct": 70.0},
	}
	headroom, ok := AccountHeadroom(usage, nil)
	if !ok || headroom != 30.0 {
		t.Fatalf("expected 100-70=30 headroom, got %v ok=%v", headroom, ok)
	}
}

func TestFetchUsageSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"five_hour": map[string]any{"utilization": 5.0},
		})
	}))
	defer srv.Close()
	withUsageURL(t, srv.URL)

	got := FetchUsage("token")
	if got == nil {
		t.Fatal("expected a usage result")
	}
	h5 := got["five_hour"].(map[string]any)
	if h5["pct"] != 5.0 {
		t.Fatalf("got %#v", got)
	}
}

func TestFetchUsageNetworkFailureReturnsNil(t *testing.T) {
	withUsageURL(t, "http://127.0.0.1:1") // nothing listens here
	if got := FetchUsage("token"); got != nil {
		t.Fatalf("expected nil on network failure, got %#v", got)
	}
}
