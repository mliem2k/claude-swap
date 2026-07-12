package cswap

import (
	"bytes"
	"strings"
	"testing"
)

func TestCLIListHumanShowsAccounts(t *testing.T) {
	s := newTestSwitcher(t)
	mustMkdir(t, GetClaudeConfigHome())
	mustWrite(t, s.GetClaudeConfigPath(), `{"oauthAccount":{"emailAddress":"a@example.com"}}`)
	mustWrite(t, GetCredentialsPath(), `{"claudeAiOauth":{"accessToken":"x"}}`)
	if _, err := s.AddAccount(nil, nil); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := RunCLI([]string{"cswap", "list"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("got exit code %d, stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "a@example.com") {
		t.Fatalf("got %q", stdout.String())
	}
}

func TestCLIListJSONOutputsPayload(t *testing.T) {
	newTestSwitcher(t)

	var stdout, stderr bytes.Buffer
	code := RunCLI([]string{"cswap", "list", "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("got exit code %d, stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"schemaVersion"`) {
		t.Fatalf("got %q", stdout.String())
	}
}

// TestCLIListJSONWithTokenStatusIsRejected mirrors main()'s parser.error():
// token status is not part of the JSON v1 schema, so the combination is
// rejected outright rather than silently dropping --token-status.
func TestCLIListJSONWithTokenStatusIsRejected(t *testing.T) {
	newTestSwitcher(t)

	var stdout, stderr bytes.Buffer
	code := RunCLI([]string{"cswap", "list", "--json", "--token-status"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("got exit code %d, want 2 (usage error); stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestCLIListNoAccountsOffersFirstRunSetupDeclined(t *testing.T) {
	s := newTestSwitcher(t)
	mustMkdir(t, GetClaudeConfigHome())
	mustWrite(t, s.GetClaudeConfigPath(), `{"oauthAccount":{"emailAddress":"a@example.com"}}`)

	var stdout, stderr bytes.Buffer
	stdin := strings.NewReader("n\n")
	code := runCLIWithStdin([]string{"cswap", "list"}, stdin, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("got exit code %d, stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "No accounts are managed yet") {
		t.Fatalf("got %q", stdout.String())
	}
	data := s.GetSequenceData()
	if data != nil && len(data.Accounts) != 0 {
		t.Fatalf("declining first-run setup should add nothing, got %#v", data.Accounts)
	}
}

func TestCLIStatusHuman(t *testing.T) {
	newTestSwitcher(t)

	var stdout, stderr bytes.Buffer
	code := RunCLI([]string{"cswap", "status"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("got exit code %d, stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Status:") {
		t.Fatalf("got %q", stdout.String())
	}
}

func TestCLIStatusJSON(t *testing.T) {
	newTestSwitcher(t)

	var stdout, stderr bytes.Buffer
	code := RunCLI([]string{"cswap", "status", "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("got exit code %d, stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"schemaVersion"`) {
		t.Fatalf("got %q", stdout.String())
	}
}
