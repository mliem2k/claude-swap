package cswap

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// Constants mirroring usage_store.py.
const (
	UsageSchemaVersion = 2

	StaleOKS     = 300.0 * float64(time.Second)
	ClaimTTLS    = 10.0 * float64(time.Second)
	TrustMaxAgeS = 3600.0 * float64(time.Second)

	BackoffBaseS        = 30.0
	BackoffCapS         = 600.0
	RetryAfterFloorCapS = 900.0

	AuthDeadStrikes = 1
)

// PermanentAuthErrors mirrors PERMANENT_AUTH_ERRORS.
var PermanentAuthErrors = map[string]bool{"invalid_grant": true}

// Identity mirrors Identity: (email, organizationUuid).
type Identity struct {
	Email            string
	OrganizationUUID string
}

// FetchRecord mirrors FetchRecord: the outcome of one fetch attempt.
type FetchRecord struct {
	Usage       map[string]any
	Error       string
	RetryAfterS *float64
	Sentinel    string
}

// UsageEntry mirrors UsageEntry: the read model of one account's usage state
// at collect time. All *float64 fields hold Unix-epoch seconds; nil means
// absent (matching Python's None).
type UsageEntry struct {
	Sentinel            *string
	LastGood            map[string]any
	FetchedAt           *float64
	AgeS                *float64
	LastAttemptAt       *float64
	ConsecutiveFailures int
	LastError           string
	BackoffUntil        *float64
	NextPollAt          *float64
	PollIntervalS       *float64
	// Last429At is when this token last answered 429 (any Retry-After).
	// Deliberately NOT cleared by a later success: the planner keeps the
	// cadence floored at poll_policy.Post429MinIntervalS until
	// poll_policy.Recent429WindowS has passed, giving the saturated
	// rolling-hour window time to age out.
	Last429At       *float64
	AuthDeadStrikes int
	TrustExtended   bool
}

// Fresh mirrors fresh(now, ttl).
func (e UsageEntry) Fresh(now time.Time, ttl time.Duration) bool {
	if e.FetchedAt == nil {
		return false
	}
	fetchedAt := time.Unix(int64(*e.FetchedAt), 0)
	return now.Sub(fetchedAt) <= ttl
}

// InBackoff mirrors in_backoff(now).
func (e UsageEntry) InBackoff(now time.Time) bool {
	if e.BackoffUntil == nil {
		return false
	}
	return now.Before(time.Unix(int64(*e.BackoffUntil), 0))
}

// Claimed mirrors claimed(now): a collector stamped this entry moments ago.
func (e UsageEntry) Claimed(now time.Time) bool {
	if e.LastAttemptAt == nil {
		return false
	}
	claimedAt := time.Unix(int64(*e.LastAttemptAt), 0)
	return now.Sub(claimedAt) < time.Duration(ClaimTTLS)
}

// TokenDead mirrors token_dead(threshold).
func (e UsageEntry) TokenDead(threshold int) bool {
	return e.AuthDeadStrikes >= threshold
}

// DecisionValue mirrors decision_value: sentinel wins; else last-good while
// trusted; else nil (unknown).
func (e UsageEntry) DecisionValue() any {
	if e.Sentinel != nil {
		return *e.Sentinel
	}
	if e.LastGood != nil && e.AgeS != nil {
		// Compared directly in float seconds (matching Python's bare
		// `self.age_s <= STALE_OK_S`), not via time.Duration(*e.AgeS):
		// that conversion truncates AgeS's fractional part before
		// multiplying by time.Second, silently flooring e.g. 300.4s to
		// 300s and widening the trust window by up to ~1s right at the
		// boundary.
		if *e.AgeS <= StaleOKS/float64(time.Second) || e.TrustExtended {
			return e.LastGood
		}
	}
	return nil
}

// DueCandidate mirrors due_candidate: the due candidate with the stalest
// data, or "" (Python's None). Due = past its NextPollAt and not in failure
// backoff; sentinel and dead-token accounts have nothing to fetch.
func DueCandidate(candidates []string, entries map[string]UsageEntry, now time.Time) string {
	type dueEntry struct {
		rank      int
		fetchedAt float64
		num       string
	}
	var due []dueEntry
	for _, num := range candidates {
		entry, ok := entries[num]
		if !ok {
			due = append(due, dueEntry{0, 0, num})
			continue
		}
		if entry.Sentinel != nil {
			continue
		}
		if entry.TokenDead(AuthDeadStrikes) {
			continue
		}
		if entry.InBackoff(now) {
			continue
		}
		if entry.NextPollAt != nil && now.Before(time.Unix(int64(*entry.NextPollAt), 0)) {
			continue
		}
		if entry.FetchedAt == nil {
			due = append(due, dueEntry{0, 0, num})
		} else {
			due = append(due, dueEntry{1, *entry.FetchedAt, num})
		}
	}
	if len(due) == 0 {
		return ""
	}
	best := due[0]
	for _, d := range due[1:] {
		switch {
		case d.rank != best.rank:
			if d.rank < best.rank {
				best = d
			}
		case d.fetchedAt != best.fetchedAt:
			if d.fetchedAt < best.fetchedAt {
				best = d
			}
		case d.num < best.num:
			// Mirrors due.sort()'s (rank, fetched_at, num) tuple sort:
			// Python's third element is the account number as a string,
			// so a full (rank, fetchedAt) tie still needs a
			// lexicographic string tiebreaker (not a numeric one, and
			// not "keep whichever was encountered first in candidates'
			// order" the way a bare min-scan would default to), or two
			// never-fetched accounts like "9" and "10" would be polled
			// in a different order than Python ("10" < "9" lexically).
			best = d
		}
	}
	return best.num
}

// failureBackoffS mirrors _failure_backoff_s.
func failureBackoffS(consecutiveFailures int, retryAfterS *float64) float64 {
	exp := consecutiveFailures - 1
	if exp < 0 {
		exp = 0
	}
	computed := BackoffBaseS
	for range exp {
		computed *= 2
		if computed >= BackoffCapS {
			computed = BackoffCapS
			break
		}
	}
	if retryAfterS == nil {
		return computed
	}
	if *retryAfterS == 0 {
		// Saturated-budget edge: wait before probing again.
		return min(max(computed, EdgeBackoffS), BackoffCapS)
	}
	floor := *retryAfterS
	if floor > RetryAfterFloorCapS {
		floor = RetryAfterFloorCapS
	}
	if floor > computed {
		return floor
	}
	return computed
}

// usageRow is the on-disk shape of one account row.
type usageRow struct {
	Email               string         `json:"email"`
	OrganizationUUID    string         `json:"organizationUuid"`
	LastGood            map[string]any `json:"lastGood,omitempty"`
	FetchedAt           *float64       `json:"fetchedAt,omitempty"`
	LastAttemptAt       *float64       `json:"lastAttemptAt,omitempty"`
	ConsecutiveFailures int            `json:"consecutiveFailures,omitempty"`
	LastError           string         `json:"lastError,omitempty"`
	BackoffUntil        *float64       `json:"backoffUntil,omitempty"`
	NextPollAt          *float64       `json:"nextPollAt,omitempty"`
	PollIntervalS       *float64       `json:"pollIntervalS,omitempty"`
	Last429At           *float64       `json:"last429At,omitempty"`
	AuthDeadStrikes     int            `json:"authDeadStrikes,omitempty"`
}

type usageTable struct {
	SchemaVersion int                 `json:"schemaVersion"`
	Accounts      map[string]usageRow `json:"accounts"`
}

// UsageStore mirrors UsageStore: the cache/usage.json table. All writes go
// read-modify-write under cache/.usage.lock; reads are lock-free (writes are
// atomic replaces).
type UsageStore struct {
	path     string
	lockPath string
	clock    func() time.Time
}

// NewUsageStore mirrors UsageStore.__init__.
func NewUsageStore(cacheDir string, clock func() time.Time) *UsageStore {
	if clock == nil {
		clock = time.Now
	}
	return &UsageStore{
		path:     filepath.Join(cacheDir, "usage.json"),
		lockPath: filepath.Join(cacheDir, ".usage.lock"),
		clock:    clock,
	}
}

// readRows mirrors _read_rows: {} on a genuine top-level parse failure,
// schema mismatch, or a non-object "accounts" value, matching Python's
// json.loads-into-a-plain-dict shape. Unlike a single strict
// json.Unmarshal into usageTable, one account row (or one field within a
// row) having an unexpected JSON type degrades only that row/field, not
// the whole table: Python's _matches/entries() apply the same kind of
// per-field isinstance() defense at read time, and the module's own
// docstring calls this out as a hard invariant ("one failed round trip no
// longer blanks every account").
func (s *UsageStore) readRows() map[string]usageRow {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return map[string]usageRow{}
	}
	var raw struct {
		SchemaVersion int            `json:"schemaVersion"`
		Accounts      map[string]any `json:"accounts"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return map[string]usageRow{}
	}
	if raw.SchemaVersion != UsageSchemaVersion {
		return map[string]usageRow{} // legacy snapshot or future schema: start empty
	}
	rows := make(map[string]usageRow, len(raw.Accounts))
	for num, v := range raw.Accounts {
		m, ok := v.(map[string]any)
		if !ok {
			continue // not an object: this one row is malformed, not the table
		}
		rows[num] = usageRowFromMap(m)
	}
	return rows
}

// usageRowFromMap defensively coerces one decoded JSON object into a
// usageRow: a field of the wrong type is dropped (zero value), matching
// Python's per-field isinstance() checks in entries() rather than failing
// the whole row.
func usageRowFromMap(m map[string]any) usageRow {
	return usageRow{
		Email:               stringFromAny(m["email"]),
		OrganizationUUID:    stringFromAny(m["organizationUuid"]),
		LastGood:            mapFromAny(m["lastGood"]),
		FetchedAt:           float64PtrFromAny(m["fetchedAt"]),
		LastAttemptAt:       float64PtrFromAny(m["lastAttemptAt"]),
		ConsecutiveFailures: intFromAny(m["consecutiveFailures"]),
		LastError:           stringFromAny(m["lastError"]),
		BackoffUntil:        float64PtrFromAny(m["backoffUntil"]),
		NextPollAt:          float64PtrFromAny(m["nextPollAt"]),
		PollIntervalS:       float64PtrFromAny(m["pollIntervalS"]),
		Last429At:           float64PtrFromAny(m["last429At"]),
		AuthDeadStrikes:     intFromAny(m["authDeadStrikes"]),
	}
}

func stringFromAny(v any) string {
	s, _ := v.(string)
	return s
}

func mapFromAny(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func float64PtrFromAny(v any) *float64 {
	f, ok := v.(float64)
	if !ok {
		return nil
	}
	return &f
}

func intFromAny(v any) int {
	f, _ := v.(float64)
	return int(f)
}

func (s *UsageStore) writeRows(rows map[string]usageRow) error {
	return atomicWriteJSON(s.path, usageTable{SchemaVersion: UsageSchemaVersion, Accounts: rows})
}

func matchesIdentity(row usageRow, identity Identity) bool {
	return row.Email == identity.Email && row.OrganizationUUID == identity.OrganizationUUID
}

func freshRow(identity Identity) usageRow {
	return usageRow{Email: identity.Email, OrganizationUUID: identity.OrganizationUUID}
}

// Entries mirrors entries: an identity-guarded snapshot for the given slots.
func (s *UsageStore) Entries(identities map[string]Identity) map[string]UsageEntry {
	now := s.clock()
	rows := s.readRows()
	out := make(map[string]UsageEntry, len(identities))
	for num, identity := range identities {
		row, ok := rows[num]
		if !ok || !matchesIdentity(row, identity) {
			out[num] = UsageEntry{}
			continue
		}
		var ageS *float64
		if row.FetchedAt != nil {
			age := now.Sub(time.Unix(int64(*row.FetchedAt), 0)).Seconds()
			ageS = &age
		}
		// A live claim keeps the trust bridge up: when another collector
		// just won the fetch, this reader must not flip trusted ->
		// unknown (and e.g. count an unhealthy tick) for the seconds the
		// result is in flight.
		claimInFlight := row.LastAttemptAt != nil &&
			now.Sub(time.Unix(int64(*row.LastAttemptAt), 0)) < time.Duration(ClaimTTLS)
		trustExtended := ageS != nil && *ageS <= TrustMaxAgeS/float64(time.Second) &&
			(row.ConsecutiveFailures > 0 ||
				(row.NextPollAt != nil && now.Before(time.Unix(int64(*row.NextPollAt), 0))) ||
				claimInFlight)
		var sentinel *string // never persisted; always nil from storage
		out[num] = UsageEntry{
			Sentinel:            sentinel,
			LastGood:            row.LastGood,
			FetchedAt:           row.FetchedAt,
			AgeS:                ageS,
			LastAttemptAt:       row.LastAttemptAt,
			ConsecutiveFailures: row.ConsecutiveFailures,
			LastError:           row.LastError,
			BackoffUntil:        row.BackoffUntil,
			NextPollAt:          row.NextPollAt,
			PollIntervalS:       row.PollIntervalS,
			Last429At:           row.Last429At,
			AuthDeadStrikes:     row.AuthDeadStrikes,
			TrustExtended:       trustExtended,
		}
	}
	return out
}

// mutate mirrors _mutate: read-modify-write rows for nums under the lock. A
// row whose stored identity mismatches is replaced with a fresh one first.
//
// A lock-acquisition failure here is silently absorbed (the call sites
// mirror Python's own uncaught-propagation shape at this depth only for
// the auto engine's tick path, which is several layers up and not worth
// threading an error return through every intermediate caller for what
// is a rare, already-bounded-by-a-generous-10s-timeout contention edge
// case); it is logged instead, mirroring the visibility Python's
// exception would otherwise give an operator, matching the
// slog.Default().Warn convention settings.go already uses for a similar
// best-effort-but-still-visible failure.
func (s *UsageStore) mutate(identities map[string]Identity, nums []string, mutator func(num string, row *usageRow)) {
	lock := NewFileLock(s.lockPath, 10*time.Second)
	if err := lock.Lock(10 * time.Second); err != nil {
		slog.Default().Warn(fmt.Sprintf("usage store: could not acquire lock for a write (%v); update dropped", err))
		return
	}
	defer lock.Release()

	rows := s.readRows()
	for _, num := range nums {
		identity := identities[num]
		row, ok := rows[num]
		if !ok || !matchesIdentity(row, identity) {
			row = freshRow(identity)
		}
		mutator(num, &row)
		rows[num] = row
	}
	_ = s.writeRows(rows)
}

// Claim mirrors claim: stamps lastAttemptAt on the slots about to be fetched.
func (s *UsageStore) Claim(nums []string, identities map[string]Identity) {
	if len(nums) == 0 {
		return
	}
	now := float64(s.clock().Unix())
	s.mutate(identities, nums, func(_ string, row *usageRow) {
		row.LastAttemptAt = &now
	})
}

// rowEligible mirrors _row_eligible: fetch eligibility of a stored row,
// evaluated under the write lock (see Reserve for the two caller modes).
func rowEligible(row usageRow, now time.Time, respectPlans bool) bool {
	if row.AuthDeadStrikes >= AuthDeadStrikes {
		return false
	}
	if row.BackoffUntil != nil && now.Before(time.Unix(int64(*row.BackoffUntil), 0)) {
		return false
	}
	if row.LastAttemptAt != nil && now.Sub(time.Unix(int64(*row.LastAttemptAt), 0)) < time.Duration(ClaimTTLS) {
		return false
	}
	stale := row.FetchedAt == nil || now.Sub(time.Unix(int64(*row.FetchedAt), 0)) > time.Duration(ServeTTLS*float64(time.Second))
	pollDue := row.NextPollAt != nil && !now.Before(time.Unix(int64(*row.NextPollAt), 0))
	if respectPlans {
		return stale && (pollDue || row.NextPollAt == nil)
	}
	return pollDue || stale
}

// Reserve mirrors reserve: atomically wins the right to fetch, re-checking
// eligibility and stamping lastAttemptAt in one locked pass, returning
// only the slots won.
//
// Deciding eligibility on a lock-free Entries read and then claiming
// separately lets two collectors both pass the check and both fetch; the
// re-check under the lock closes that window. Eligibility: not
// quarantined (dead token), not in failure backoff, not claimed within
// ClaimTTLS, and then by caller mode:
//
//   - respectPlans=true (on-demand callers: list/status/switch,
//     dashboards): the entry must be stale (older than ServeTTLS) AND
//     poll-due (past NextPollAt, or no plan yet).
//   - respectPlans=false (the auto engine's deliberate schedule):
//     poll-due OR stale (a due entry may be re-fetched inside the serve
//     TTL, that is how the bounded urgent cadence beats the TTL), and an
//     escalation refresh may fetch a not-yet-due candidate.
func (s *UsageStore) Reserve(nums []string, identities map[string]Identity, respectPlans bool) []string {
	if len(nums) == 0 {
		return nil
	}
	lock := NewFileLock(s.lockPath, 10*time.Second)
	if err := lock.Lock(10 * time.Second); err != nil {
		slog.Default().Warn(fmt.Sprintf("usage store: could not acquire lock to reserve a fetch (%v); this pass fetches nothing", err))
		return nil
	}
	defer lock.Release()

	now := s.clock()
	nowS := float64(now.Unix())
	rows := s.readRows()
	var won []string
	for _, num := range nums {
		identity := identities[num]
		row, ok := rows[num]
		if !ok || !matchesIdentity(row, identity) {
			row = freshRow(identity)
			rows[num] = row
		} else if !rowEligible(row, now, respectPlans) {
			continue
		}
		row.LastAttemptAt = &nowS
		rows[num] = row
		won = append(won, num)
	}
	if len(won) > 0 {
		_ = s.writeRows(rows)
	}
	return won
}

// Record mirrors record: merges fetch outcomes. Success and failure are
// mutually exclusive writers; sentinel records are no-ops.
func (s *UsageStore) Record(outcomes map[string]FetchRecord, identities map[string]Identity) {
	effective := make(map[string]FetchRecord, len(outcomes))
	var nums []string
	for num, rec := range outcomes {
		if rec.Sentinel == "" {
			effective[num] = rec
			nums = append(nums, num)
		}
	}
	if len(effective) == 0 {
		return
	}
	now := float64(s.clock().Unix())
	s.mutate(identities, nums, func(num string, row *usageRow) {
		rec := effective[num]
		row.LastAttemptAt = &now
		if rec.Error == "" {
			row.LastGood = rec.Usage
			row.FetchedAt = &now
			row.ConsecutiveFailures = 0
			row.LastError = ""
			row.BackoffUntil = nil
			row.AuthDeadStrikes = 0
		} else {
			row.ConsecutiveFailures++
			row.LastError = rec.Error
			if rec.Error == "http-429" {
				// Kept across later successes: the poll planner floors
				// the cadence while a 429 is recent (UsageEntry.Last429At).
				row.Last429At = &now
			}
			backoff := now + failureBackoffS(row.ConsecutiveFailures, rec.RetryAfterS)
			row.BackoffUntil = &backoff
			if PermanentAuthErrors[rec.Error] {
				row.AuthDeadStrikes++
			}
		}
	})
}

// SetPollPlan mirrors set_poll_plan: persists the scheduler's per-slot
// (nextPollAt, pollIntervalS).
func (s *UsageStore) SetPollPlan(plans map[string][2]*float64, identities map[string]Identity) {
	if len(plans) == 0 {
		return
	}
	nums := make([]string, 0, len(plans))
	for num := range plans {
		nums = append(nums, num)
	}
	s.mutate(identities, nums, func(num string, row *usageRow) {
		plan := plans[num]
		row.NextPollAt = plan[0]
		row.PollIntervalS = plan[1]
	})
}

// ClearDeadToken mirrors clear_dead_token: lifts the dead-token quarantine
// for slots whose credential was refreshed.
func (s *UsageStore) ClearDeadToken(nums []string, identities map[string]Identity) {
	if len(nums) == 0 {
		return
	}
	s.mutate(identities, nums, func(_ string, row *usageRow) {
		row.AuthDeadStrikes = 0
		row.ConsecutiveFailures = 0
		row.LastError = ""
		row.BackoffUntil = nil
	})
}

// WithSentinel mirrors with_sentinel: overlays a derived sentinel state on a
// stored entry (read model only).
func WithSentinel(entry UsageEntry, sentinel string) UsageEntry {
	if sentinel == "" {
		return entry
	}
	entry.Sentinel = &sentinel
	return entry
}
