package cswap

import "testing"

func TestSnapshotSourceTakeNormalPassAllowsFetch(t *testing.T) {
	s := setupOneActiveAccountForCollectScheduled(t)
	src := NewSnapshotSource(s)

	snap := src.Take(false, false)

	if len(snap.Accounts) != 1 {
		t.Fatalf("got %d accounts, want 1", len(snap.Accounts))
	}
}

func TestSnapshotSourceTakeStoreOnlyFetchesNothing(t *testing.T) {
	s := setupOneActiveAccountForCollectScheduled(t)
	src := NewSnapshotSource(s)

	// store-only must not error and must still return a coherent snapshot
	// built entirely from whatever the store already has cached.
	snap := src.Take(false, true)

	if len(snap.Accounts) != 1 {
		t.Fatalf("got %d accounts, want 1", len(snap.Accounts))
	}
}

func TestSnapshotSourceTakeRemembersLast(t *testing.T) {
	s := setupOneActiveAccountForCollectScheduled(t)
	src := NewSnapshotSource(s)

	snap := src.Take(true, false)

	if src.last == nil || src.last.TakenAt != snap.TakenAt {
		t.Fatal("Take did not record its result as the last snapshot")
	}
}
