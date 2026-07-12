package cswap

import (
	"bytes"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

func writeTestEnvelope(t *testing.T, path string, envelope TransferEnvelope) {
	t.Helper()
	data, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, path, string(data))
}

func oneAccountEnvelope() TransferEnvelope {
	num := 1
	return TransferEnvelope{
		Version:             TransferFormatVersion,
		ExportedAt:          "2026-01-01T00:00:00Z",
		ExportedFrom:        "macos",
		SwapVersion:         "1.0.0",
		Encrypted:           false,
		ActiveAccountNumber: &num,
		Accounts: []TransferAccount{
			{
				Number:           1,
				Email:            "a@example.com",
				UUID:             "u1",
				OrganizationUUID: "org1",
				OrganizationName: "Acme",
				Added:            "2026-01-01T00:00:00Z",
				Credentials:      map[string]any{"claudeAiOauth": map[string]any{"accessToken": "x"}},
				Config:           map[string]any{"oauthAccount": map[string]any{"emailAddress": "a@example.com"}},
			},
		},
	}
}

func TestImportAccountsFreshImport(t *testing.T) {
	s := newTestSwitcher(t)
	path := filepath.Join(t.TempDir(), "in.json")
	writeTestEnvelope(t, path, oneAccountEnvelope())
	var stderr bytes.Buffer

	if err := s.ImportAccounts(path, false, &stderr); err != nil {
		t.Fatal(err)
	}
	data := s.GetSequenceData()
	if data == nil || len(data.Accounts) != 1 {
		t.Fatalf("got %#v", data)
	}
	if data.Accounts["1"].Email != "a@example.com" {
		t.Fatalf("got %#v", data.Accounts["1"])
	}
	if data.ActiveAccountNumber == nil || *data.ActiveAccountNumber != 1 {
		t.Fatalf("expected activeAccountNumber seeded to 1, got %#v", data.ActiveAccountNumber)
	}
}

func TestImportAccountsSkipsExistingWithoutForce(t *testing.T) {
	s := newTestSwitcher(t)
	path := filepath.Join(t.TempDir(), "in.json")
	writeTestEnvelope(t, path, oneAccountEnvelope())
	var stderr bytes.Buffer
	if err := s.ImportAccounts(path, false, &stderr); err != nil {
		t.Fatal(err)
	}
	stderr.Reset()
	if err := s.ImportAccounts(path, false, &stderr); err != nil {
		t.Fatal(err)
	}
	if !contains(stderr.String(), "already exists") {
		t.Fatalf("expected a skip notice, got %q", stderr.String())
	}
}

func TestImportAccountsForceOverwrites(t *testing.T) {
	s := newTestSwitcher(t)
	path := filepath.Join(t.TempDir(), "in.json")
	writeTestEnvelope(t, path, oneAccountEnvelope())
	var stderr bytes.Buffer
	if err := s.ImportAccounts(path, false, &stderr); err != nil {
		t.Fatal(err)
	}
	stderr.Reset()
	if err := s.ImportAccounts(path, true, &stderr); err != nil {
		t.Fatal(err)
	}
	if !contains(stderr.String(), "Overwrote") {
		t.Fatalf("expected an overwrite notice, got %q", stderr.String())
	}
}

func TestImportAccountsWrongVersionIsError(t *testing.T) {
	s := newTestSwitcher(t)
	path := filepath.Join(t.TempDir(), "in.json")
	env := oneAccountEnvelope()
	env.Version = 99
	writeTestEnvelope(t, path, env)
	var stderr bytes.Buffer
	err := s.ImportAccounts(path, false, &stderr)
	if !errors.Is(err, ErrTransfer) {
		t.Fatalf("got %v", err)
	}
}

func TestImportAccountsEncryptedIsError(t *testing.T) {
	s := newTestSwitcher(t)
	path := filepath.Join(t.TempDir(), "in.json")
	env := oneAccountEnvelope()
	env.Encrypted = true
	writeTestEnvelope(t, path, env)
	var stderr bytes.Buffer
	err := s.ImportAccounts(path, false, &stderr)
	if !errors.Is(err, ErrTransfer) {
		t.Fatalf("got %v", err)
	}
}

func TestImportAccountsNoAccountsIsError(t *testing.T) {
	s := newTestSwitcher(t)
	path := filepath.Join(t.TempDir(), "in.json")
	env := oneAccountEnvelope()
	env.Accounts = nil
	writeTestEnvelope(t, path, env)
	var stderr bytes.Buffer
	err := s.ImportAccounts(path, false, &stderr)
	if !errors.Is(err, ErrTransfer) {
		t.Fatalf("got %v", err)
	}
}

func TestImportAccountsDuplicateInFileIsError(t *testing.T) {
	s := newTestSwitcher(t)
	path := filepath.Join(t.TempDir(), "in.json")
	env := oneAccountEnvelope()
	dup := env.Accounts[0]
	dup.Number = 2
	env.Accounts = append(env.Accounts, dup)
	writeTestEnvelope(t, path, env)
	var stderr bytes.Buffer
	err := s.ImportAccounts(path, false, &stderr)
	if !errors.Is(err, ErrTransfer) {
		t.Fatalf("got %v", err)
	}
	// Pass-1 validation must reject before any write: no accounts.json
	// entries should exist afterward.
	data := s.GetSequenceData()
	if data != nil && len(data.Accounts) != 0 {
		t.Fatalf("expected no partial writes on validation failure, got %#v", data.Accounts)
	}
}

func TestImportAccountsApiKeyAccount(t *testing.T) {
	s := newTestSwitcher(t)
	path := filepath.Join(t.TempDir(), "in.json")
	env := oneAccountEnvelope()
	env.Accounts[0].Credentials = "sk-ant-api03-fake-key-content-1234567890"
	env.Accounts[0].Kind = "api_key"
	writeTestEnvelope(t, path, env)
	var stderr bytes.Buffer
	if err := s.ImportAccounts(path, false, &stderr); err != nil {
		t.Fatal(err)
	}
	data := s.GetSequenceData()
	if data.Accounts["1"].Kind != "api_key" {
		t.Fatalf("got %#v", data.Accounts["1"])
	}
}

func TestImportAccountsAssignsNextFreeSlotOnCollision(t *testing.T) {
	s := newTestSwitcher(t)
	if err := s.SetupDirectories(); err != nil {
		t.Fatal(err)
	}
	if err := s.InitSequenceFile(); err != nil {
		t.Fatal(err)
	}
	data := s.GetSequenceData()
	data.Sequence = []int{1}
	data.Accounts["1"] = AccountRecord{Email: "occupant@example.com"}
	if err := s.WriteJSON(s.SequenceFile, data); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "in.json")
	writeTestEnvelope(t, path, oneAccountEnvelope()) // exported as slot 1, but 1 is occupied by a different email
	var stderr bytes.Buffer
	if err := s.ImportAccounts(path, false, &stderr); err != nil {
		t.Fatal(err)
	}
	final := s.GetSequenceData()
	if _, stillOccupant := final.Accounts["1"]; !stillOccupant || final.Accounts["1"].Email != "occupant@example.com" {
		t.Fatalf("expected slot 1 untouched, got %#v", final.Accounts["1"])
	}
	if final.Accounts["2"].Email != "a@example.com" {
		t.Fatalf("expected the imported account bumped to slot 2, got %#v", final.Accounts)
	}
}
