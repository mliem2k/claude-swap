package cswap

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestParsePayloadValidObject(t *testing.T) {
	got, err := parsePayload(`{"a":1}`, "test")
	if err != nil {
		t.Fatal(err)
	}
	if got["a"] != 1.0 {
		t.Fatalf("got %#v", got)
	}
}

func TestParsePayloadInvalidJSONIsTransferError(t *testing.T) {
	_, err := parsePayload("not json", "test label")
	if !errors.Is(err, ErrTransfer) {
		t.Fatalf("got %v", err)
	}
}

func TestParsePayloadNonObjectIsTransferError(t *testing.T) {
	_, err := parsePayload(`[1,2,3]`, "test label")
	if !errors.Is(err, ErrTransfer) {
		t.Fatalf("got %v", err)
	}
}

func TestSlimConfigKeepsOnlyOauthAccount(t *testing.T) {
	got, err := slimConfig(map[string]any{
		"oauthAccount": map[string]any{"emailAddress": "a@example.com"},
		"userID":       "secret-machine-id",
	}, "test")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %#v", got)
	}
	oauth, ok := got["oauthAccount"].(map[string]any)
	if !ok || oauth["emailAddress"] != "a@example.com" {
		t.Fatalf("got %#v", got)
	}
}

func TestSlimConfigMissingOauthAccountIsTransferError(t *testing.T) {
	_, err := slimConfig(map[string]any{"userID": "x"}, "test label")
	if !errors.Is(err, ErrTransfer) {
		t.Fatalf("got %v", err)
	}
}

func TestAtomicWriteFileWritesContentAt0600(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")
	if err := atomicWriteFile(path, "hello\n"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello\n" {
		t.Fatalf("got %q", data)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("got perm %v", info.Mode().Perm())
	}
}

func TestValidateImportedAccountValidEntry(t *testing.T) {
	s := newTestSwitcher(t)
	email, number, err := validateImportedAccount(s, map[string]any{
		"email": "a@example.com", "number": 1.0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if email != "a@example.com" || number != "1" {
		t.Fatalf("got %q %q", email, number)
	}
}

func TestValidateImportedAccountInvalidEmailIsTransferError(t *testing.T) {
	s := newTestSwitcher(t)
	_, _, err := validateImportedAccount(s, map[string]any{
		"email": "not-an-email", "number": 1.0,
	})
	if !errors.Is(err, ErrTransfer) {
		t.Fatalf("got %v", err)
	}
}

func TestValidateImportedAccountMissingNumberIsTransferError(t *testing.T) {
	s := newTestSwitcher(t)
	_, _, err := validateImportedAccount(s, map[string]any{
		"email": "a@example.com",
	})
	if !errors.Is(err, ErrTransfer) {
		t.Fatalf("got %v", err)
	}
}

func TestValidateImportedAccountZeroNumberIsTransferError(t *testing.T) {
	s := newTestSwitcher(t)
	_, _, err := validateImportedAccount(s, map[string]any{
		"email": "a@example.com", "number": 0.0,
	})
	if !errors.Is(err, ErrTransfer) {
		t.Fatalf("got %v", err)
	}
}

func TestValidateImportedAccountNonStringOptionalFieldIsTransferError(t *testing.T) {
	s := newTestSwitcher(t)
	_, _, err := validateImportedAccount(s, map[string]any{
		"email": "a@example.com", "number": 1.0, "organizationUuid": 123.0,
	})
	if !errors.Is(err, ErrTransfer) {
		t.Fatalf("got %v", err)
	}
}
