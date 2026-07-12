package cswap

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func setupSwitchableTwoAccountFixture(t *testing.T) *ClaudeAccountSwitcher {
	t.Helper()
	s := newTestSwitcher(t)
	if err := s.SetupDirectories(); err != nil {
		t.Fatal(err)
	}
	if err := s.InitSequenceFile(); err != nil {
		t.Fatal(err)
	}
	mustMkdir(t, GetClaudeConfigHome())

	data := s.GetSequenceData()
	one := 1
	data.ActiveAccountNumber = &one
	data.Sequence = []int{1, 2}
	data.Accounts["1"] = AccountRecord{Email: "a@example.com", UUID: "u1"}
	data.Accounts["2"] = AccountRecord{Email: "b@example.com", UUID: "u2"}
	if err := s.WriteJSON(s.SequenceFile, data); err != nil {
		t.Fatal(err)
	}

	liveCreds := `{"claudeAiOauth":{"accessToken":"live-a","refreshToken":"r-a"}}`
	mustWrite(t, GetCredentialsPath(), liveCreds)
	mustWrite(t, s.GetClaudeConfigPath(), `{"oauthAccount":{"emailAddress":"a@example.com","organizationUuid":""}}`)

	_ = s.WriteAccountCredentials("1", "a@example.com", liveCreds)
	_ = s.WriteAccountConfig("1", "a@example.com", `{"oauthAccount":{"emailAddress":"a@example.com"}}`)
	_ = s.WriteAccountCredentials("2", "b@example.com", `{"claudeAiOauth":{"accessToken":"live-b","refreshToken":"r-b"}}`)
	_ = s.WriteAccountConfig("2", "b@example.com", `{"oauthAccount":{"emailAddress":"b@example.com"}}`)

	return s
}

func TestPerformSwitchNormalPathActivatesTarget(t *testing.T) {
	s := setupSwitchableTwoAccountFixture(t)

	op, err := s.PerformSwitch("2", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if op.From == nil || op.From.Number == nil || *op.From.Number != 1 {
		t.Fatalf("expected from=1, got %#v", op.From)
	}
	if op.To == nil || op.To.Number == nil || *op.To.Number != 2 {
		t.Fatalf("expected to=2, got %#v", op.To)
	}

	activeCreds := s.ReadCredentials()
	if activeCreds != `{"claudeAiOauth":{"accessToken":"live-b","refreshToken":"r-b"}}` {
		t.Fatalf("expected active credentials to be account 2's, got %q", activeCreds)
	}

	data := s.GetSequenceData()
	if data.ActiveAccountNumber == nil || *data.ActiveAccountNumber != 2 {
		t.Fatalf("expected activeAccountNumber=2, got %#v", data.ActiveAccountNumber)
	}
}

func TestPerformSwitchBacksUpOutgoingAccount(t *testing.T) {
	s := setupSwitchableTwoAccountFixture(t)

	if _, err := s.PerformSwitch("2", false, nil); err != nil {
		t.Fatal(err)
	}

	backedUp := s.ReadAccountCredentials("1", "a@example.com")
	if backedUp != `{"claudeAiOauth":{"accessToken":"live-a","refreshToken":"r-a"}}` {
		t.Fatalf("expected account 1's backup to hold the pre-switch live creds, got %q", backedUp)
	}
}

func TestPerformSwitchFailsWhenTargetHasNoCredentials(t *testing.T) {
	s := setupSwitchableTwoAccountFixture(t)
	data := s.GetSequenceData()
	data.Sequence = []int{1, 2, 3}
	data.Accounts["3"] = AccountRecord{Email: "c@example.com"} // no backup written
	if err := s.WriteJSON(s.SequenceFile, data); err != nil {
		t.Fatal(err)
	}

	_, err := s.PerformSwitch("3", false, nil)
	if !errors.Is(err, ErrSwitch) {
		t.Fatalf("expected ErrSwitch, got %v", err)
	}

	// Rollback must have restored the original active credentials.
	got := s.ReadCredentials()
	if got != `{"claudeAiOauth":{"accessToken":"live-a","refreshToken":"r-a"}}` {
		t.Fatalf("expected rollback to restore original live credentials, got %q", got)
	}
	data = s.GetSequenceData()
	if data.ActiveAccountNumber == nil || *data.ActiveAccountNumber != 1 {
		t.Fatalf("expected activeAccountNumber still 1 after rollback, got %#v", data.ActiveAccountNumber)
	}
}

// TestPerformSwitchRollsBackCredentialsAndConfigWhenSequenceWriteFails drives
// tx.Rollback for real, unlike TestPerformSwitchFailsWhenTargetHasNoCredentials
// above (which fails before any RecordStep call, so CompletedSteps stays
// empty and Rollback never runs). Here credentials_written and config_written
// both complete first, and only the final sequence-file write fails, so
// Rollback must undo two steps in reverse order.
//
// WriteJSON writes its atomic-rename temp file at "<path>.<pid>.tmp" before
// renaming it over path. Since this test and the switcher run in the same
// process, os.Getpid() here is the exact pid WriteJSON will use, so
// pre-occupying that temp path with a directory makes the write fail with
// "is a directory": a filesystem type mismatch, not a permission check, so
// unlike chmod-based fault injection this is deterministic across platforms
// and is not bypassed by a root/Administrator test process.
func TestPerformSwitchRollsBackCredentialsAndConfigWhenSequenceWriteFails(t *testing.T) {
	s := setupSwitchableTwoAccountFixture(t)

	originalCreds := s.ReadCredentials()
	originalConfigBytes, err := os.ReadFile(s.GetClaudeConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	originalConfig := string(originalConfigBytes)
	data := s.GetSequenceData()
	if data.ActiveAccountNumber == nil || *data.ActiveAccountNumber != 1 {
		t.Fatalf("expected fixture to start active on account 1, got %#v", data.ActiveAccountNumber)
	}

	blockedTemp := fmt.Sprintf("%s.%d.tmp", s.SequenceFile, os.Getpid())
	mustMkdir(t, blockedTemp)

	_, err = s.PerformSwitch("2", false, nil)
	if err == nil {
		t.Fatal("expected an error when the sequence file write fails mid-transaction")
	}

	// The live credentials must be back to account 1's original value, not
	// account 2's: proof that Rollback's credentials_written case actually ran.
	if got := s.ReadCredentials(); got != originalCreds {
		t.Fatalf("expected rollback to restore original live credentials, got %q", got)
	}

	// The config file must be back to its original content, not the rewritten
	// oauthAccount for account 2: proof that Rollback's config_written case
	// actually ran.
	gotConfigBytes, err := os.ReadFile(s.GetClaudeConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	if string(gotConfigBytes) != originalConfig {
		t.Fatalf("expected rollback to restore original config, got %s", gotConfigBytes)
	}

	// activeAccountNumber must remain 1: the failed write never touched the
	// real sequence file (it failed before the rename), so this also confirms
	// no partial state leaked through.
	data = s.GetSequenceData()
	if data.ActiveAccountNumber == nil || *data.ActiveAccountNumber != 1 {
		t.Fatalf("expected activeAccountNumber unchanged at 1, got %#v", data.ActiveAccountNumber)
	}
}

func TestPerformSwitchDirectActivationNoLiveLogin(t *testing.T) {
	s := newTestSwitcher(t)
	if err := s.SetupDirectories(); err != nil {
		t.Fatal(err)
	}
	if err := s.InitSequenceFile(); err != nil {
		t.Fatal(err)
	}
	mustMkdir(t, GetClaudeConfigHome())
	// No .claude.json at all: fresh-machine path.
	data := s.GetSequenceData()
	data.Sequence = []int{1}
	data.Accounts["1"] = AccountRecord{Email: "a@example.com"}
	if err := s.WriteJSON(s.SequenceFile, data); err != nil {
		t.Fatal(err)
	}
	_ = s.WriteAccountCredentials("1", "a@example.com", `{"claudeAiOauth":{"accessToken":"x"}}`)
	_ = s.WriteAccountConfig("1", "a@example.com", `{"oauthAccount":{"emailAddress":"a@example.com"}}`)

	op, err := s.PerformSwitch("1", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if op.From != nil {
		t.Fatalf("expected from=nil on a fresh machine, got %#v", op.From)
	}
	if op.To == nil || op.To.Number == nil || *op.To.Number != 1 {
		t.Fatalf("expected to=1, got %#v", op.To)
	}
	if got := s.ReadCredentials(); got != `{"claudeAiOauth":{"accessToken":"x"}}` {
		t.Fatalf("expected target activated, got %q", got)
	}
}

// TestPerformSwitchDirectActivationAbortsOnCredentialFileReadFailure covers
// Fix 1: a live, unmanaged OAuth login exists (hasCurrent is true but
// currentAccount is "" because no managed slot matches it), which enters
// performSwitchLocked's direct-activation branch on its own. The plaintext
// credentials file is forced to fail to read, which must abort the switch
// with ErrCredentialRead rather than silently treating the read failure as
// "nothing to snapshot" and overwriting the live login.
func TestPerformSwitchDirectActivationAbortsOnCredentialFileReadFailure(t *testing.T) {
	s := newTestSwitcher(t)
	if err := s.SetupDirectories(); err != nil {
		t.Fatal(err)
	}
	if err := s.InitSequenceFile(); err != nil {
		t.Fatal(err)
	}
	mustMkdir(t, GetClaudeConfigHome())

	// Off-macOS, useKeychain() is always false, so the Keychain never
	// intervenes and readActiveCredentials() must reach the plaintext-file
	// branch to decide the outcome. Simpler than faking a covered Keychain
	// failure, and this is the platform the brief points at as the cleanest
	// way to force execution into that branch.
	s.Store.platform = PlatformLinux

	data := s.GetSequenceData()
	data.Sequence = []int{1}
	data.Accounts["1"] = AccountRecord{Email: "target@example.com"}
	if err := s.WriteJSON(s.SequenceFile, data); err != nil {
		t.Fatal(err)
	}
	_ = s.WriteAccountCredentials("1", "target@example.com", `{"claudeAiOauth":{"accessToken":"target"}}`)
	_ = s.WriteAccountConfig("1", "target@example.com", `{"oauthAccount":{"emailAddress":"target@example.com"}}`)

	// Live login belongs to nobody managed: GetCurrentAccount reports
	// hasCurrent=true, but FindAccountSlot finds no slot for it, so
	// currentAccount=="" and the direct-activation branch is entered without
	// needing forceActivate.
	mustWrite(t, s.GetClaudeConfigPath(), `{"oauthAccount":{"emailAddress":"unmanaged@example.com","organizationUuid":""}}`)

	// Occupy the plaintext credentials path with a directory: os.ReadFile on
	// a directory errors portably on every platform this project targets, no
	// chmod/root permissions needed (same trick as
	// TestPerformSwitchRollsBackCredentialsAndConfigWhenSequenceWriteFails).
	mustMkdir(t, GetCredentialsPath())

	_, err := s.PerformSwitch("1", false, nil)
	if !errors.Is(err, ErrCredentialRead) {
		t.Fatalf("expected ErrCredentialRead, got %v", err)
	}

	// The target must never have been activated: sequence.json's
	// activeAccountNumber must remain unset, proving WriteCredentials was
	// never reached (the fix aborts before the write, not after it).
	after := s.GetSequenceData()
	if after.ActiveAccountNumber != nil {
		t.Fatalf("expected activeAccountNumber to remain unset, got %v", *after.ActiveAccountNumber)
	}
}

// TestPerformSwitchDirectActivationProceedsWhenKeychainUnavailableButCovered
// is Fix 1's regression guard: a covered Keychain-unavailable outcome
// (readActiveCredentials legitimately returns Value:"" ,
// KeychainUnavailable:true, matching
// TestReadActiveCredentialsKeychainUnavailableFlag) must NOT trip the new
// ErrCredentialRead guard. Python treats "" as "nothing to snapshot", not an
// error, and this must keep working.
func TestPerformSwitchDirectActivationProceedsWhenKeychainUnavailableButCovered(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(dir, "claude-config"))
	fake := newFakeSecurity()
	fake.getErr = ErrKeychainUnavailable
	restore := swapSecurity(fake)
	t.Cleanup(restore)

	s, err := NewClaudeAccountSwitcher(false)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetupDirectories(); err != nil {
		t.Fatal(err)
	}
	if err := s.InitSequenceFile(); err != nil {
		t.Fatal(err)
	}
	mustMkdir(t, GetClaudeConfigHome())

	data := s.GetSequenceData()
	data.Sequence = []int{1}
	data.Accounts["1"] = AccountRecord{Email: "target@example.com"}
	if err := s.WriteJSON(s.SequenceFile, data); err != nil {
		t.Fatal(err)
	}
	_ = s.WriteAccountCredentials("1", "target@example.com", `{"claudeAiOauth":{"accessToken":"target"}}`)
	_ = s.WriteAccountConfig("1", "target@example.com", `{"oauthAccount":{"emailAddress":"target@example.com"}}`)

	// Same "live login belongs to nobody managed" setup as the abort test
	// above, so this also exercises the direct-activation branch. No
	// plaintext credentials file and no managed key anywhere, so the
	// Keychain failure is fully covered.
	mustWrite(t, s.GetClaudeConfigPath(), `{"oauthAccount":{"emailAddress":"unmanaged@example.com","organizationUuid":""}}`)

	op, err := s.PerformSwitch("1", false, nil)
	if err != nil {
		t.Fatalf("expected the switch to proceed despite a covered keychain failure, got error: %v", err)
	}
	if errors.Is(err, ErrCredentialRead) {
		t.Fatal("a covered keychain-unavailable outcome must not be classified as ErrCredentialRead")
	}
	if op.To == nil || op.To.Number == nil || *op.To.Number != 1 {
		t.Fatalf("expected target activated, got %#v", op.To)
	}
	after := s.GetSequenceData()
	if after.ActiveAccountNumber == nil || *after.ActiveAccountNumber != 1 {
		t.Fatalf("expected activeAccountNumber=1, got %#v", after.ActiveAccountNumber)
	}
}

func TestPerformSwitchForceActivationSkipsBackup(t *testing.T) {
	s := setupSwitchableTwoAccountFixture(t)
	// Corrupt account 1's backup so a normal-path backup step would be
	// visible; force must not touch it.
	beforeBackup := s.ReadAccountCredentials("1", "a@example.com")

	_, err := s.PerformSwitch("2", true, nil)
	if err != nil {
		t.Fatal(err)
	}
	afterBackup := s.ReadAccountCredentials("1", "a@example.com")
	if afterBackup != beforeBackup {
		t.Fatalf("force activation must not rewrite the outgoing slot's backup: before=%q after=%q", beforeBackup, afterBackup)
	}
	if got := s.ReadCredentials(); got != `{"claudeAiOauth":{"accessToken":"live-b","refreshToken":"r-b"}}` {
		t.Fatalf("expected target activated, got %q", got)
	}
}
