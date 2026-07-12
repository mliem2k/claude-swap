package cswap

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func setupOneOAuthAccountForTransfer(t *testing.T) (*ClaudeAccountSwitcher, string) {
	t.Helper()
	s := newTestSwitcher(t)
	mustMkdir(t, GetClaudeConfigHome())
	mustWrite(t, s.GetClaudeConfigPath(), `{"oauthAccount":{"emailAddress":"a@example.com"},"userID":"secret"}`)
	mustWrite(t, GetCredentialsPath(), `{"claudeAiOauth":{"accessToken":"x","refreshToken":"r"}}`)
	if _, err := s.AddAccount(nil, nil); err != nil {
		t.Fatal(err)
	}
	return s, "a@example.com"
}

func TestExportAccountsToStdoutSlimByDefault(t *testing.T) {
	s, email := setupOneOAuthAccountForTransfer(t)
	var stderr bytes.Buffer

	dest := filepath.Join(t.TempDir(), "out.json")
	if err := s.ExportAccounts(dest, "", false, &stderr); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	var envelope TransferEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Version != TransferFormatVersion {
		t.Fatalf("got %d", envelope.Version)
	}
	if len(envelope.Accounts) != 1 || envelope.Accounts[0].Email != email {
		t.Fatalf("got %#v", envelope.Accounts)
	}
	config := envelope.Accounts[0].Config
	if len(config) != 1 {
		t.Fatalf("expected slimmed config (oauthAccount only), got %#v", config)
	}
	if _, hasSecret := config["userID"]; hasSecret {
		t.Fatal("expected userID stripped from slimmed config")
	}
}

func TestExportAccountsFullIncludesEverything(t *testing.T) {
	s, _ := setupOneOAuthAccountForTransfer(t)
	var stderr bytes.Buffer
	dest := filepath.Join(t.TempDir(), "out.json")
	if err := s.ExportAccounts(dest, "", true, &stderr); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	var envelope TransferEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatal(err)
	}
	if _, hasSecret := envelope.Accounts[0].Config["userID"]; !hasSecret {
		t.Fatal("expected --full to keep userID")
	}
}

func TestExportAccountsNoAccountsIsTransferError(t *testing.T) {
	s := newTestSwitcher(t)
	var stderr bytes.Buffer
	err := s.ExportAccounts(filepath.Join(t.TempDir(), "out.json"), "", false, &stderr)
	if !errors.Is(err, ErrTransfer) {
		t.Fatalf("got %v", err)
	}
}

func TestExportAccountsExplicitAccountNotFoundIsError(t *testing.T) {
	s, _ := setupOneOAuthAccountForTransfer(t)
	var stderr bytes.Buffer
	err := s.ExportAccounts(filepath.Join(t.TempDir(), "out.json"), "99", false, &stderr)
	if err == nil {
		t.Fatal("expected an error for a nonexistent account")
	}
}

func TestExportAccountsApiKeyAccountCarriesRawString(t *testing.T) {
	s := newTestSwitcher(t)
	// AddAccount snapshots the live credential and (like Python's
	// add_account/_reject_live_api_key_capture) unconditionally rejects a raw
	// sk-ant-api... credential, directing callers to add_account_from_token
	// instead. AddAccountFromToken is the API both ports actually use to
	// register a managed API key, matching Python's own
	// test_api_key_accounts.py::TestExportImport setup.
	if _, err := s.AddAccountFromToken("sk-ant-api03-fake-key-content-1234567890", "a@example.com", nil, nil); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	dest := filepath.Join(t.TempDir(), "out.json")
	if err := s.ExportAccounts(dest, "", false, &stderr); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(dest)
	var envelope TransferEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Accounts[0].Kind != "api_key" {
		t.Fatalf("got %#v", envelope.Accounts[0])
	}
	credsStr, ok := envelope.Accounts[0].Credentials.(string)
	if !ok || credsStr == "" {
		t.Fatalf("expected a raw string credential, got %#v", envelope.Accounts[0].Credentials)
	}
}

func TestExportAccountsToStdoutHyphen(t *testing.T) {
	s, _ := setupOneOAuthAccountForTransfer(t)
	var stderr bytes.Buffer
	if err := s.ExportAccounts("-", "", false, &stderr); err != nil {
		t.Fatal(err)
	}
	// "-" writes to os.Stdout directly per Python parity (sys.stdout.write);
	// this test only confirms it doesn't error and doesn't write a file
	// literally named "-".
	if _, err := os.Stat("-"); err == nil {
		t.Fatal("expected no file literally named \"-\" to be created")
		_ = os.Remove("-")
	}
}
