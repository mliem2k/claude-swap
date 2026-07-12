package cswap

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

// TestLogUsageFailure429MessageMentionsPerTokenBudgetWithRetryAfter verifies
// that a 429 with a Retry-After value gets the per-token-budget explanation.
func TestLogUsageFailure429MessageMentionsPerTokenBudgetWithRetryAfter(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	retryAfter := 5.0
	logUsageFailure(logger, "", errors.New("boom"), "http-429", &retryAfter)

	msg := buf.String()
	if !strings.Contains(msg, "per-token usage budget") {
		t.Fatalf("got %q, expected the per-token-budget explanation", msg)
	}
}

// TestLogUsageFailure429MessageAppliesEvenWithoutRetryAfter verifies that
// the per-token-budget explanation applies to every 429, not just ones that
// carried a Retry-After header, matching the new understanding that any of
// cswap's own polling can trigger the per-token usage budget.
func TestLogUsageFailure429MessageAppliesEvenWithoutRetryAfter(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	logUsageFailure(logger, "", errors.New("boom"), "http-429", nil)

	msg := buf.String()
	if !strings.Contains(msg, "per-token usage budget") {
		t.Fatalf("got %q, expected the per-token-budget explanation even when retryAfterS is nil", msg)
	}
}

// TestLogUsageFailureNon429MessageOmitsPerTokenBudget verifies that the
// per-token-budget explanation is specific to http-429 and is not appended
// to other failure kinds.
func TestLogUsageFailureNon429MessageOmitsPerTokenBudget(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	logUsageFailure(logger, "", errors.New("boom"), "timeout", nil)

	msg := buf.String()
	if strings.Contains(msg, "per-token usage budget") {
		t.Fatalf("got %q, did not expect the per-token-budget explanation for a non-429 kind", msg)
	}
}
