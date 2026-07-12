package cswap

// SnapshotSource mirrors SnapshotSource (snapshot_source.py): the
// supported read path for dashboards and GUI shells. Pacing is
// store-governed (UsageStore.Reserve's freshness/backoff/claim gates cap
// every surface at the same per-token cadence), so a dashboard repainting
// every few seconds and a one-shot `cswap list` produce identical network
// behavior. Take just runs the same on-demand pass `cswap list` does
// (fetch=nil); the store decides which accounts, if any, may actually be
// fetched. storeOnly is for shells that host an auto engine, which
// already collects on its own schedule.
//
// Take is blocking (file locks, keychain subprocesses, network): call it
// from a goroutine, never a UI update loop.
type SnapshotSource struct {
	switcher *ClaudeAccountSwitcher
	last     *AccountsSnapshotResult
}

// NewSnapshotSource mirrors SnapshotSource.__init__.
func NewSnapshotSource(switcher *ClaudeAccountSwitcher) *SnapshotSource {
	return &SnapshotSource{switcher: switcher}
}

// Take mirrors take(full, store_only). full is accepted for API
// stability but is no faster than a normal pass: even an explicit
// refresh is capped by the store's serve TTL and poll plans. storeOnly
// reads the store without any network eligibility.
func (s *SnapshotSource) Take(full, storeOnly bool) AccountsSnapshotResult {
	_ = full
	var fetch map[string]bool
	if storeOnly {
		fetch = map[string]bool{}
	}
	snap := s.switcher.AccountsSnapshot(fetch)
	s.last = &snap
	return snap
}
