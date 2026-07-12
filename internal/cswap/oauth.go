package cswap

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Constants mirroring oauth.py.
const (
	OAuthBetaHeader     = "oauth-2025-04-20"
	OAuthExpiryBufferMs = 5 * 60 * 1000
	OAuthClientID       = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
)

// Endpoint URLs as vars so tests can point them at a local httptest.Server.
var (
	oauthTokenURL   = "https://platform.claude.com/v1/oauth/token"
	oauthProfileURL = "https://api.anthropic.com/api/oauth/profile"
	usageAPIURL     = "https://api.anthropic.com/api/oauth/usage"
)

// ExtractAccessToken mirrors extract_access_token. "" when absent or the
// credentials string does not parse.
func ExtractAccessToken(credentials string) string {
	data := ExtractOAuthData(credentials)
	if data == nil {
		return ""
	}
	token, _ := data["accessToken"].(string)
	return token
}

// ExtractOAuthData mirrors extract_oauth_data: the claudeAiOauth payload, or
// nil when the credentials string does not parse or carries no such object.
func ExtractOAuthData(credentials string) map[string]any {
	var data map[string]any
	if err := json.Unmarshal([]byte(credentials), &data); err != nil {
		return nil
	}
	oauth, ok := data["claudeAiOauth"].(map[string]any)
	if !ok {
		return nil
	}
	return oauth
}

// CredentialFingerprint mirrors credential_fingerprint: a stable identity
// fingerprint. Refresh-token hash when one exists (survives access-token
// rotation); full-content hash otherwise (API keys and setup tokens never
// rotate, so content identity is lineage identity). "" only for empty input.
func CredentialFingerprint(credentials string) string {
	if credentials == "" {
		return ""
	}
	data := ExtractOAuthData(credentials)
	if data != nil {
		if token, ok := data["refreshToken"].(string); ok && token != "" {
			sum := sha256.Sum256([]byte(token))
			return "sha256:" + hex.EncodeToString(sum[:])
		}
	}
	sum := sha256.Sum256([]byte(credentials))
	return "sha256-full:" + hex.EncodeToString(sum[:])
}

// IsOAuthTokenExpired mirrors is_oauth_token_expired.
func IsOAuthTokenExpired(expiresAt any) bool {
	var ms float64
	switch v := expiresAt.(type) {
	case float64:
		ms = v
	case int:
		ms = float64(v)
	case int64:
		ms = float64(v)
	default:
		return false
	}
	nowMs := float64(time.Now().UTC().UnixMilli())
	return nowMs+OAuthExpiryBufferMs >= ms
}

// FormatReset mirrors format_reset: (countdown, clock) for a reset time in
// local time.
func FormatReset(resetsAt string) (string, string, error) {
	resetUTC, err := time.Parse(time.RFC3339, resetsAt)
	if err != nil {
		return "", "", err
	}
	now := time.Now().UTC()
	remaining := resetUTC.Sub(now)
	totalSeconds := max(int(remaining.Seconds()), 0)
	days := totalSeconds / 86400
	rem := totalSeconds % 86400
	hours := rem / 3600
	rem %= 3600
	minutes := rem / 60

	var countdown string
	switch {
	case days > 0:
		countdown = fmt.Sprintf("%dd %dh", days, hours)
	case hours > 0:
		countdown = fmt.Sprintf("%dh %dm", hours, minutes)
	default:
		countdown = fmt.Sprintf("%dm", minutes)
	}

	return countdown, ResetClockString(resetUTC, now), nil
}

// ResetClockString mirrors reset_clock_string: absolute reset time in
// local time, "20:39" same-day, else "Jan 2 15:04".
func ResetClockString(resetUTC, nowUTC time.Time) string {
	resetLocal := resetUTC.Local()
	nowLocal := nowUTC.Local()
	if resetLocal.Year() == nowLocal.Year() && resetLocal.YearDay() == nowLocal.YearDay() {
		return resetLocal.Format("15:04")
	}
	return resetLocal.Format("Jan 2 15:04")
}

// FreshResetStrings mirrors fresh_reset_strings: (countdown, clock, ok) for
// one usage window. Recomputed from resets_at at render time; falls back to
// the fetch-time cached strings when resets_at is absent or unparseable.
func FreshResetStrings(window map[string]any) (string, string, bool) {
	if resetsAt, ok := window["resets_at"].(string); ok && resetsAt != "" {
		if countdown, clock, err := FormatReset(resetsAt); err == nil {
			return countdown, clock, true
		}
	}
	if clock, ok := window["clock"].(string); ok {
		countdown, _ := window["countdown"].(string)
		if countdown == "" {
			countdown = "?"
		}
		return countdown, clock, true
	}
	return "", "", false
}

// BuildTokenStatus mirrors build_token_status: a short debug summary of
// stored OAuth token state. "" when the credentials carry no OAuth payload.
func BuildTokenStatus(credentials string) string {
	oauth := ExtractOAuthData(credentials)
	if oauth == nil {
		return ""
	}
	hasRefresh := false
	if token, ok := oauth["refreshToken"].(string); ok && token != "" {
		hasRefresh = true
	}
	refreshStr := "no"
	if hasRefresh {
		refreshStr = "yes"
	}
	expiresAt, ok := oauth["expiresAt"]
	if !ok {
		return fmt.Sprintf("oauth: unknown expiry, refresh token %s", refreshStr)
	}
	ms, ok := expiresAt.(float64)
	if !ok {
		return fmt.Sprintf("oauth: unknown expiry, refresh token %s", refreshStr)
	}
	expiresUTC := time.UnixMilli(int64(ms)).UTC()
	state := "fresh"
	if IsOAuthTokenExpired(ms) {
		state = "expired"
	}
	countdown, clock, err := FormatReset(expiresUTC.Format(time.RFC3339))
	if err != nil {
		return fmt.Sprintf("oauth: %s, refresh token %s, unknown expiry", state, refreshStr)
	}
	return fmt.Sprintf("oauth: %s, refresh token %s, expires %s in %s", state, refreshStr, clock, countdown)
}

// RefreshOutcome mirrors RefreshOutcome. Error is "" on success, else one of
// "invalid_grant", "no_refresh_token", "transient".
type RefreshOutcome struct {
	Credentials  string
	Error        string
	TokenAccount map[string]any
}

// TryRefreshOAuthCredentials mirrors try_refresh_oauth_credentials.
func TryRefreshOAuthCredentials(credentials string) RefreshOutcome {
	var data map[string]any
	if err := json.Unmarshal([]byte(credentials), &data); err != nil {
		return RefreshOutcome{Error: "no_refresh_token"}
	}
	oauth, _ := data["claudeAiOauth"].(map[string]any)
	refreshToken, _ := oauth["refreshToken"].(string)
	if oauth == nil || refreshToken == "" {
		return RefreshOutcome{Error: "no_refresh_token"}
	}

	body, _ := json.Marshal(map[string]any{
		"grant_type":    "refresh_token",
		"refresh_token": refreshToken,
		"client_id":     OAuthClientID,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, oauthTokenURL, bytes.NewReader(body))
	if err != nil {
		return RefreshOutcome{Error: "transient"}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "claude-swap/1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return RefreshOutcome{Error: "transient"}
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 300 {
		bodyStr := string(respBody)
		if (resp.StatusCode == 400 || resp.StatusCode == 401 || resp.StatusCode == 403) &&
			(strings.Contains(bodyStr, "invalid_grant") || strings.Contains(bodyStr, "invalid_client")) {
			return RefreshOutcome{Error: "invalid_grant"}
		}
		return RefreshOutcome{Error: "transient"}
	}

	var respData map[string]any
	if err := json.Unmarshal(respBody, &respData); err != nil {
		return RefreshOutcome{Error: "transient"}
	}

	nowMs := float64(time.Now().UTC().UnixMilli())
	accessToken, _ := respData["access_token"].(string)
	expiresIn, _ := respData["expires_in"].(float64)
	oauth["accessToken"] = accessToken
	oauth["expiresAt"] = nowMs + expiresIn*1000
	if rt, ok := respData["refresh_token"].(string); ok && rt != "" {
		oauth["refreshToken"] = rt
	}
	if scope, ok := respData["scope"].(string); ok && scope != "" {
		oauth["scopes"] = strings.Fields(scope)
	}
	data["claudeAiOauth"] = oauth

	rotated, err := json.Marshal(data)
	if err != nil {
		return RefreshOutcome{Error: "transient"}
	}
	return RefreshOutcome{Credentials: string(rotated), TokenAccount: parseTokenAccount(respData)}
}

// parseTokenAccount mirrors _parse_token_account.
func parseTokenAccount(respData map[string]any) map[string]any {
	account, _ := respData["account"].(map[string]any)
	if account == nil {
		return nil
	}
	uuid, _ := account["uuid"].(string)
	uuid = strings.TrimSpace(uuid)
	if uuid == "" {
		return nil
	}
	email, _ := account["email_address"].(string)
	organization, _ := respData["organization"].(map[string]any)
	orgUUID, _ := organization["uuid"].(string)
	return map[string]any{
		"uuid":             uuid,
		"email":            emptyToNilString(email),
		"organizationUuid": emptyToNilString(orgUUID),
	}
}

// emptyToNilString mirrors the Python "value if isinstance(value, str) else
// None" pattern: an absent/empty optional field reads as nil, not "".
func emptyToNilString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// RefreshOAuthCredentials mirrors refresh_oauth_credentials: "" on any failure.
func RefreshOAuthCredentials(credentials string) string {
	return TryRefreshOAuthCredentials(credentials).Credentials
}

// FetchOAuthProfile mirrors fetch_oauth_profile: resolves an OAuth access
// token to its account identity, or nil on any failure. Must not be called
// while any credential/config lock is held (network under locks is
// forbidden).
func FetchOAuthProfile(accessToken string) map[string]any {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, oauthProfileURL, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "claude-swap/1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var data map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil
	}
	account, _ := data["account"].(map[string]any)
	if account == nil {
		return nil
	}
	uuid, _ := account["uuid"].(string)
	uuid = strings.TrimSpace(uuid)
	if uuid == "" {
		return nil
	}
	email, _ := account["email"].(string)
	organization, _ := data["organization"].(map[string]any)
	orgUUID, _ := organization["uuid"].(string)
	return map[string]any{
		"uuid":             uuid,
		"email":            emptyToNilString(email),
		"organizationUuid": emptyToNilString(orgUUID),
	}
}

// RequestUsageData mirrors request_usage_data: raw utilization data from the
// Anthropic usage API.
func RequestUsageData(accessToken string) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, usageAPIURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("anthropic-beta", OAuthBetaHeader)
	req.Header.Set("User-Agent", "claude-swap/1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &httpStatusError{code: resp.StatusCode, retryAfter: resp.Header.Get("Retry-After")}
	}
	var data map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}
	return data, nil
}

// httpStatusError mirrors urllib.error.HTTPError for classification
// purposes: it carries the status code and any Retry-After header.
type httpStatusError struct {
	code       int
	retryAfter string
}

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("http %d", e.code)
}

// classifyUsageError mirrors _classify_usage_error: (kind, retryAfterS).
func classifyUsageError(err error) (string, *float64) {
	var statusErr *httpStatusError
	if errors.As(err, &statusErr) {
		var retryAfter *float64
		if statusErr.retryAfter != "" {
			if v, perr := strconv.ParseFloat(strings.TrimSpace(statusErr.retryAfter), 64); perr == nil && v >= 0 {
				retryAfter = &v
			}
		}
		return fmt.Sprintf("http-%d", statusErr.code), retryAfter
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "timeout", nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout", nil
	}
	var syntaxErr *json.SyntaxError
	if errors.As(err, &syntaxErr) {
		return "bad-response", nil
	}
	return "network", nil
}

// logUsageFailure mirrors _log_usage_failure: one WARNING line with the
// cause, the full error at DEBUG. context must not carry an email (the line
// is what users paste into public issues).
func logUsageFailure(logger *slog.Logger, context string, err error, kind string, retryAfterS *float64) {
	where := ""
	if context != "" {
		where = " " + context
	}
	cause := kind
	if retryAfterS != nil {
		cause = fmt.Sprintf("%s, retry-after %.0fs", kind, *retryAfterS)
	}
	if kind == "http-429" {
		cause += " (per-token usage budget reached, backing off)"
	}
	logger.Warn(fmt.Sprintf("usage fetch failed%s: %s", where, cause))
	logger.Debug(fmt.Sprintf("usage fetch failure detail%s: %v", where, err))
}

// BuildUsageResult mirrors build_usage_result: normalizes raw usage API data.
// Returns nil when the response carried no recognized window data.
func BuildUsageResult(data map[string]any) map[string]any {
	result := map[string]any{}

	if h5, ok := data["five_hour"].(map[string]any); ok {
		result["five_hour"] = buildWindowEntry(h5, "utilization")
	}
	if d7, ok := data["seven_day"].(map[string]any); ok {
		result["seven_day"] = buildWindowEntry(d7, "utilization")
	}

	if eu, ok := data["extra_usage"].(map[string]any); ok {
		if enabled, _ := eu["is_enabled"].(bool); enabled {
			usedCredits, uOK := eu["used_credits"].(float64)
			monthlyLimit, mOK := eu["monthly_limit"].(float64)
			utilization, pOK := eu["utilization"].(float64)
			if uOK && mOK && pOK {
				spend := map[string]any{
					"used":     usedCredits / 100,
					"limit":    monthlyLimit / 100,
					"pct":      utilization,
					"currency": stringOr(eu["currency"], "USD"),
				}
				if resetsAt, ok := eu["resets_at"].(string); ok && resetsAt != "" {
					spend["resets_at"] = resetsAt
					if countdown, clock, err := FormatReset(resetsAt); err == nil {
						spend["countdown"] = countdown
						spend["clock"] = clock
					}
				}
				result["spend"] = spend
			}
		}
	}

	if limits, ok := data["limits"].([]any); ok {
		var scoped []map[string]any
		for _, raw := range limits {
			lim, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			scope, _ := lim["scope"].(map[string]any)
			model, _ := scope["model"].(map[string]any)
			name, _ := model["display_name"].(string)
			pct, pctOK := lim["percent"].(float64)
			if name == "" || !pctOK {
				continue
			}
			entry := map[string]any{"name": name, "pct": pct}
			if resetsAt, ok := lim["resets_at"].(string); ok && resetsAt != "" {
				entry["resets_at"] = resetsAt
				if countdown, clock, err := FormatReset(resetsAt); err == nil {
					entry["countdown"] = countdown
					entry["clock"] = clock
				}
			}
			scoped = append(scoped, entry)
		}
		if len(scoped) > 0 {
			result["scoped"] = scoped
		}
	}

	if len(result) == 0 {
		return nil
	}
	return result
}

// ScopedWindows extracts a usage map's "scoped" (per-model weekly limit)
// entries, tolerating both shapes it can genuinely carry: a native Go
// []map[string]any, as this file's own AccountUsage constructs on a fresh
// fetch (see the "scoped" assignment above), and the []any-of-map[string]any
// shape any value carries after a JSON round trip (encoding/json never
// decodes a JSON array as []map[string]any, only []any with map[string]any
// elements). A bare `usage["scoped"].([]map[string]any)` assertion silently
// returns nothing whenever usage came from UsageStore rather than a fetch
// this exact tick, which given the whole point of the store is to serve
// paced, cached data is most of the time, not an edge case: every scoped
// (per-model) limit and its "(!)" maxed-out marker would otherwise vanish
// from cswap list, --json output, the TUI, and the menu bar for any
// account whose usage wasn't just now fetched.
func ScopedWindows(usage map[string]any) []map[string]any {
	switch v := usage["scoped"].(type) {
	case []map[string]any:
		return v
	case []any:
		out := make([]map[string]any, 0, len(v))
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	default:
		return nil
	}
}

// buildWindowEntry builds a five_hour/seven_day entry: {pct, [resets_at,
// countdown, clock]}.
func buildWindowEntry(window map[string]any, pctKey string) map[string]any {
	entry := map[string]any{"pct": window[pctKey]}
	if resetsAt, ok := window["resets_at"].(string); ok && resetsAt != "" {
		entry["resets_at"] = resetsAt
		if countdown, clock, err := FormatReset(resetsAt); err == nil {
			entry["countdown"] = countdown
			entry["clock"] = clock
		}
	}
	return entry
}

func stringOr(v any, fallback string) string {
	if s, ok := v.(string); ok && s != "" {
		return s
	}
	return fallback
}

// Window mirrors one (label, pct, resets_at) tuple from relevant_windows.
type Window struct {
	Label    string
	Pct      float64
	ResetsAt string
}

// RelevantWindows mirrors relevant_windows: every window that gates this
// account. Always 5h and 7d; each named per-model scoped window is included
// when models is non-empty (case-insensitive match; "all" matches every
// scoped window).
func RelevantWindows(usage map[string]any, models []string) []Window {
	if usage == nil {
		return nil
	}
	var windows []Window
	for _, pair := range [][2]string{{"five_hour", "5h"}, {"seven_day", "7d"}} {
		if w, ok := usage[pair[0]].(map[string]any); ok {
			if pct, ok := w["pct"].(float64); ok {
				resetsAt, _ := w["resets_at"].(string)
				windows = append(windows, Window{Label: pair[1], Pct: pct, ResetsAt: resetsAt})
			}
		}
	}
	if len(models) > 0 {
		wanted := map[string]bool{}
		matchAll := false
		for _, m := range models {
			lower := strings.ToLower(m)
			wanted[lower] = true
			if lower == "all" {
				matchAll = true
			}
		}
		for _, s := range ScopedWindows(usage) {
			name, _ := s["name"].(string)
			pct, pctOK := s["pct"].(float64)
			if name == "" || !pctOK {
				continue
			}
			if matchAll || wanted[strings.ToLower(name)] {
				resetsAt, _ := s["resets_at"].(string)
				windows = append(windows, Window{Label: name, Pct: pct, ResetsAt: resetsAt})
			}
		}
	}
	return windows
}

// AccountHeadroom mirrors account_headroom: (100 - max(pct), true), or
// (0, false) when there is no window data (the caller's "unknown").
func AccountHeadroom(usage map[string]any, models []string) (float64, bool) {
	windows := RelevantWindows(usage, models)
	if len(windows) == 0 {
		return 0, false
	}
	maxPct := windows[0].Pct
	for _, w := range windows[1:] {
		if w.Pct > maxPct {
			maxPct = w.Pct
		}
	}
	return 100.0 - maxPct, true
}

// FetchUsage mirrors fetch_usage: nil on any failure.
func FetchUsage(accessToken string) map[string]any {
	data, err := RequestUsageData(accessToken)
	if err != nil {
		kind, _ := classifyUsageError(err)
		logUsageFailure(slog.Default(), "", err, kind, nil)
		return nil
	}
	return BuildUsageResult(data)
}

// UsageOutcome mirrors UsageOutcome: the result of a usage-API fetch attempt.
type UsageOutcome struct {
	Usage       map[string]any
	Error       string
	RetryAfterS *float64
}

// TryFetchUsageForAccount mirrors try_fetch_usage_for_account: fetches usage
// for an account, refreshing expired tokens for inactive accounts only.
// Active accounts are never refreshed (Claude Code owns those credentials).
func TryFetchUsageForAccount(
	accountNum, email, credentials string,
	isActive bool,
	persistCredentials func(accountNum, email, credentials string) error,
	logger *slog.Logger,
) UsageOutcome {
	ctxLabel := fmt.Sprintf("for account %s", accountNum) // no email: paste-safe
	oauth := ExtractOAuthData(credentials)
	accessToken, _ := oauth["accessToken"].(string)
	if accessToken == "" {
		return UsageOutcome{Error: "no-access-token"}
	}

	workingCredentials := credentials
	refreshToken, _ := oauth["refreshToken"].(string)

	if !isActive && refreshToken != "" && IsOAuthTokenExpired(oauth["expiresAt"]) {
		refresh := TryRefreshOAuthCredentials(workingCredentials)
		if refresh.Credentials != "" {
			workingCredentials = refresh.Credentials
			persistRefreshed(persistCredentials, accountNum, email, workingCredentials, logger)
			if newOAuth := ExtractOAuthData(workingCredentials); newOAuth != nil {
				oauth = newOAuth
				if tok, ok := oauth["accessToken"].(string); ok && tok != "" {
					accessToken = tok
				}
			}
		} else if refresh.Error == "invalid_grant" {
			return UsageOutcome{Error: "invalid_grant"}
		}
		// A transient refresh failure falls through to try the (expired)
		// token; the 401 path below retries the refresh.
	}

	data, err := RequestUsageData(accessToken)
	if err == nil {
		return UsageOutcome{Usage: BuildUsageResult(data)}
	}

	kind, retryAfter := classifyUsageError(err)
	var statusErr *httpStatusError
	is401 := errors.As(err, &statusErr) && statusErr.code == 401
	if !is401 || isActive || oauth == nil || refreshToken == "" {
		logUsageFailure(logger, ctxLabel, err, kind, retryAfter)
		return UsageOutcome{Error: kind, RetryAfterS: retryAfter}
	}

	refresh := TryRefreshOAuthCredentials(workingCredentials)
	if refresh.Credentials == "" {
		logUsageFailure(logger, ctxLabel, err, kind, nil)
		if refresh.Error == "invalid_grant" {
			return UsageOutcome{Error: "invalid_grant"}
		}
		return UsageOutcome{Error: "refresh-failed"}
	}

	workingCredentials = refresh.Credentials
	persistRefreshed(persistCredentials, accountNum, email, workingCredentials, logger)
	refreshedOAuth := ExtractOAuthData(workingCredentials)
	newToken, _ := refreshedOAuth["accessToken"].(string)
	if newToken == "" {
		return UsageOutcome{Error: "refresh-failed"}
	}

	retryData, retryErr := RequestUsageData(newToken)
	if retryErr != nil {
		kind, retryAfter := classifyUsageError(retryErr)
		logUsageFailure(logger, ctxLabel+" after refresh", retryErr, kind, retryAfter)
		return UsageOutcome{Error: kind, RetryAfterS: retryAfter}
	}
	return UsageOutcome{Usage: BuildUsageResult(retryData)}
}

// FetchUsageForAccount mirrors fetch_usage_for_account: the Usage field or
// nil (see TryFetchUsageForAccount for the cause).
func FetchUsageForAccount(
	accountNum, email, credentials string,
	isActive bool,
	persistCredentials func(accountNum, email, credentials string) error,
	logger *slog.Logger,
) map[string]any {
	return TryFetchUsageForAccount(accountNum, email, credentials, isActive, persistCredentials, logger).Usage
}

// persistRefreshed mirrors _persist: calls the persist callback, warning
// loudly on failure. Console warning printing (Python's printer.warning) is
// deferred to Plan 5 (printer.go); this logs via the passed logger only.
func persistRefreshed(
	callback func(accountNum, email, credentials string) error,
	accountNum, email, credentials string,
	logger *slog.Logger,
) {
	if callback == nil {
		return
	}
	if err := callback(accountNum, email, credentials); err != nil {
		logger.Warn(fmt.Sprintf(
			"refreshed oauth token for account %s (%s) but failed to persist it: %v; "+
				"the refresh token on disk may now be stale, re-run cswap add --slot N after logging in if the next refresh fails",
			accountNum, email, err))
	}
}
