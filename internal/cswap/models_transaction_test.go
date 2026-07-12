package cswap

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestSwitchTransactionRecordStep(t *testing.T) {
	tx := &SwitchTransaction{}
	tx.RecordStep("credentials_written")
	tx.RecordStep("config_written")
	if len(tx.CompletedSteps) != 2 || tx.CompletedSteps[1] != "config_written" {
		t.Fatalf("got %#v", tx.CompletedSteps)
	}
}

func TestSwitchTransactionRollbackRestoresConfigFile(t *testing.T) {
	s := newTestSwitcher(t)
	if err := s.SetupDirectories(); err != nil {
		t.Fatal(err)
	}
	mustMkdir(t, GetClaudeConfigHome())
	configPath := s.GetClaudeConfigPath()
	mustWrite(t, configPath, `{"original":true}`)

	tx := &SwitchTransaction{
		OriginalConfig: `{"original":true}`,
		ConfigPath:     configPath,
	}
	// Simulate a partial write that needs undoing.
	mustWrite(t, configPath, `{"partial":true}`)
	tx.RecordStep("config_written")

	if ok := tx.Rollback(s); !ok {
		t.Fatal("expected rollback to succeed")
	}
	got, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"original":true}` {
		t.Fatalf("expected config restored, got %s", got)
	}
}

func TestSwitchTransactionRollbackRestoresCredentials(t *testing.T) {
	s := newTestSwitcher(t)
	if err := s.SetupDirectories(); err != nil {
		t.Fatal(err)
	}
	mustMkdir(t, GetClaudeConfigHome())
	original := `{"claudeAiOauth":{"accessToken":"orig"}}`
	tx := &SwitchTransaction{OriginalCredentials: original}
	// Simulate the switch having written new (wrong) credentials.
	if err := s.WriteCredentials(`{"claudeAiOauth":{"accessToken":"new"}}`); err != nil {
		t.Fatal(err)
	}
	tx.RecordStep("credentials_written")

	if ok := tx.Rollback(s); !ok {
		t.Fatal("expected rollback to succeed")
	}
	got := s.ReadCredentials()
	if got != original {
		t.Fatalf("expected credentials restored, got %q", got)
	}
}

func TestSwitchTransactionRollbackRestoresSequenceFile(t *testing.T) {
	s := newTestSwitcher(t)
	if err := s.SetupDirectories(); err != nil {
		t.Fatal(err)
	}
	if err := s.InitSequenceFile(); err != nil {
		t.Fatal(err)
	}
	data := s.GetSequenceData()
	one := 1
	data.ActiveAccountNumber = &one
	if err := s.WriteJSON(s.SequenceFile, data); err != nil {
		t.Fatal(err)
	}

	tx := &SwitchTransaction{OriginalAccountNum: "1"}
	// Simulate the switch having advanced activeAccountNumber to 2.
	two := 2
	data.ActiveAccountNumber = &two
	if err := s.WriteJSON(s.SequenceFile, data); err != nil {
		t.Fatal(err)
	}
	tx.RecordStep("sequence_updated")

	if ok := tx.Rollback(s); !ok {
		t.Fatal("expected rollback to succeed")
	}
	got := s.GetSequenceData()
	if got.ActiveAccountNumber == nil || *got.ActiveAccountNumber != 1 {
		t.Fatalf("expected activeAccountNumber restored to 1, got %#v", got.ActiveAccountNumber)
	}
}

func TestSwitchTransactionRollbackFailsOnUnparseableAccountNum(t *testing.T) {
	s := newTestSwitcher(t)
	if err := s.SetupDirectories(); err != nil {
		t.Fatal(err)
	}
	if err := s.InitSequenceFile(); err != nil {
		t.Fatal(err)
	}
	data := s.GetSequenceData()
	one := 1
	data.ActiveAccountNumber = &one
	if err := s.WriteJSON(s.SequenceFile, data); err != nil {
		t.Fatal(err)
	}

	tx := &SwitchTransaction{OriginalAccountNum: "not-a-number"}
	// Simulate the switch having advanced activeAccountNumber to 2.
	two := 2
	data.ActiveAccountNumber = &two
	if err := s.WriteJSON(s.SequenceFile, data); err != nil {
		t.Fatal(err)
	}
	tx.RecordStep("sequence_updated")

	// Capture the sequence file's raw bytes before rollback. On a parse
	// failure the fixed code must log the error and continue without ever
	// reaching the WriteJSON call, so the file on disk must stay byte-for-byte
	// identical. We compare raw file contents rather than the LastUpdated
	// timestamp because LastUpdated has only 1-second resolution and this
	// whole test runs in well under a second: a same-second WriteJSON call
	// would refresh LastUpdated to a value that is indistinguishable from the
	// pre-rollback value at that resolution, so a timestamp comparison would
	// pass even against a buggy implementation that still calls WriteJSON on
	// this path. A byte-for-byte file comparison has no such blind spot: if
	// WriteJSON is never called, the file cannot change, full stop.
	before, err := os.ReadFile(s.SequenceFile)
	if err != nil {
		t.Fatal(err)
	}

	if ok := tx.Rollback(s); ok {
		t.Fatal("expected rollback to fail on unparseable OriginalAccountNum")
	}
	got := s.GetSequenceData()
	if got.ActiveAccountNumber == nil || *got.ActiveAccountNumber != 2 {
		t.Fatalf("expected activeAccountNumber to remain untouched at 2, got %#v", got.ActiveAccountNumber)
	}
	after, err := os.ReadFile(s.SequenceFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("expected sequence file to be untouched (WriteJSON not called) on parse failure, but file contents changed from %q to %q", before, after)
	}
}

func TestSwitchTransactionRollbackRunsStepsInReverseOrder(t *testing.T) {
	s := newTestSwitcher(t)
	if err := s.SetupDirectories(); err != nil {
		t.Fatal(err)
	}
	if err := s.InitSequenceFile(); err != nil {
		t.Fatal(err)
	}
	mustMkdir(t, GetClaudeConfigHome())
	configPath := s.GetClaudeConfigPath()
	mustWrite(t, configPath, `{"original":true}`)

	tx := &SwitchTransaction{
		OriginalCredentials: `{"claudeAiOauth":{"accessToken":"orig"}}`,
		OriginalConfig:      `{"original":true}`,
		OriginalAccountNum:  "1",
		ConfigPath:          configPath,
	}
	data := s.GetSequenceData()
	one := 1
	data.ActiveAccountNumber = &one
	_ = s.WriteJSON(s.SequenceFile, data)

	if err := s.WriteCredentials(`{"claudeAiOauth":{"accessToken":"new"}}`); err != nil {
		t.Fatal(err)
	}
	tx.RecordStep("credentials_written")
	mustWrite(t, configPath, `{"partial":true}`)
	tx.RecordStep("config_written")
	two := 2
	data.ActiveAccountNumber = &two
	_ = s.WriteJSON(s.SequenceFile, data)
	tx.RecordStep("sequence_updated")

	if ok := tx.Rollback(s); !ok {
		t.Fatal("expected rollback to succeed")
	}
	if got := s.ReadCredentials(); got != tx.OriginalCredentials {
		t.Fatalf("credentials not restored: %q", got)
	}
	configGot, _ := os.ReadFile(configPath)
	if string(configGot) != tx.OriginalConfig {
		t.Fatalf("config not restored: %s", configGot)
	}
	seqGot := s.GetSequenceData()
	if seqGot.ActiveAccountNumber == nil || *seqGot.ActiveAccountNumber != 1 {
		t.Fatalf("sequence not restored: %#v", seqGot.ActiveAccountNumber)
	}
}

var _ = filepath.Join // keep import used if a future edit trims a use above
