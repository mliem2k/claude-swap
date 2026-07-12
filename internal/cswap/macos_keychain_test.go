package cswap

import (
	"errors"
	"testing"
)

// fakeSecurity records calls and returns canned results, standing in for
// the /usr/bin/security binary so tests never touch the real Keychain.
type fakeSecurity struct {
	items     map[string]string // "service\x00account" -> password
	getErr    error
	setErr    error
	deleteErr error
}

func newFakeSecurity() *fakeSecurity {
	return &fakeSecurity{items: map[string]string{}}
}

func key(service, account string) string { return service + "\x00" + account }

func (f *fakeSecurity) GetPassword(service, account string) (string, error) {
	if f.getErr != nil {
		return "", f.getErr
	}
	v, ok := f.items[key(service, account)]
	if !ok {
		return "", ErrKeychainNotFound
	}
	return v, nil
}

func (f *fakeSecurity) SetPassword(service, account, password string) error {
	if f.setErr != nil {
		return f.setErr
	}
	f.items[key(service, account)] = password
	return nil
}

func (f *fakeSecurity) DeletePassword(service, account string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	delete(f.items, key(service, account))
	return nil
}

func (f *fakeSecurity) ItemExists(service, account string) bool {
	_, ok := f.items[key(service, account)]
	return ok
}

func TestGetPasswordMissingIsNotFound(t *testing.T) {
	restore := swapSecurity(newFakeSecurity())
	defer restore()
	_, err := GetPassword("svc", "acct")
	if !errors.Is(err, ErrKeychainNotFound) {
		t.Fatalf("expected ErrKeychainNotFound, got %v", err)
	}
}

func TestSetGetDeleteRoundTrip(t *testing.T) {
	fake := newFakeSecurity()
	restore := swapSecurity(fake)
	defer restore()

	if err := SetPassword("svc", "acct", "hunter2"); err != nil {
		t.Fatal(err)
	}
	got, err := GetPassword("svc", "acct")
	if err != nil || got != "hunter2" {
		t.Fatalf("round trip got=%q err=%v", got, err)
	}
	if !ItemExists("svc", "acct") {
		t.Fatal("ItemExists should be true after set")
	}
	if err := DeletePassword("svc", "acct"); err != nil {
		t.Fatal(err)
	}
	if ItemExists("svc", "acct") {
		t.Fatal("ItemExists should be false after delete")
	}
}

func TestDeleteAbsentIsSuccess(t *testing.T) {
	// rc 44 (already absent) counts as success, so deleting a missing item
	// must not return ErrKeychainNotFound.
	restore := swapSecurity(newFakeSecurity())
	defer restore()
	if err := DeletePassword("svc", "never-set"); err != nil {
		t.Fatalf("delete of absent item should succeed, got %v", err)
	}
}

func TestIsUnavailableClassifiesErrors(t *testing.T) {
	if !IsUnavailable(ErrKeychainUnavailable) {
		t.Fatal("ErrKeychainUnavailable should be unavailable")
	}
	if IsUnavailable(ErrKeychainNotFound) {
		t.Fatal("not-found is NOT unavailable (a real miss, not a failure)")
	}
	// A wrapped unavailability (errors.Join) must still classify as unavailable.
	joined := errors.Join(ErrKeychainUnavailable, errors.New("rc=1"))
	if !IsUnavailable(joined) {
		t.Fatal("wrapped ErrKeychainUnavailable should be unavailable")
	}
}

func TestKeychainAccountNamePrefersUserEnv(t *testing.T) {
	t.Setenv("USER", "alice")
	if got := KeychainAccountName(); got != "alice" {
		t.Fatalf("expected USER env, got %q", got)
	}
}
