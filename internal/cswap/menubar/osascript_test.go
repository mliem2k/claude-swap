package menubar

import (
	"errors"
	"strings"
	"testing"
)

type fakeOsascript struct {
	lastScript string
	stdout     string
	err        error
}

func (f *fakeOsascript) run(script string) (string, error) {
	f.lastScript = script
	return f.stdout, f.err
}

func TestTextInputDialogParsesReturnedText(t *testing.T) {
	f := &fakeOsascript{stdout: "button returned:Next, text returned:hello@example.com\n"}
	value, ok := TextInputDialog(f, "Title", "Message")
	if !ok || value != "hello@example.com" {
		t.Errorf("TextInputDialog = (%q, %v)", value, ok)
	}
	if !strings.Contains(f.lastScript, "Message") || !strings.Contains(f.lastScript, "Title") {
		t.Errorf("script sent to osascript = %q, want it to reference the title/message", f.lastScript)
	}
}

func TestTextInputDialogCancelled(t *testing.T) {
	// osascript returns a nonzero exit (surfaced as an error by exec.Cmd)
	// when the user clicks Cancel on a `display dialog`.
	f := &fakeOsascript{err: errors.New("exit status 1")}
	_, ok := TextInputDialog(f, "Title", "Message")
	if ok {
		t.Error("TextInputDialog on cancel should return ok=false")
	}
}

func TestTextInputDialogEmptyAnswerIsNotOK(t *testing.T) {
	f := &fakeOsascript{stdout: "button returned:Next, text returned:\n"}
	_, ok := TextInputDialog(f, "Title", "Message")
	if ok {
		t.Error("TextInputDialog with an empty answer should return ok=false")
	}
}

func TestConfirmDialogConfirmed(t *testing.T) {
	f := &fakeOsascript{stdout: "button returned:Remove\n"}
	if !ConfirmDialog(f, "Title", "Message", "Remove") {
		t.Error("ConfirmDialog should be true when the confirm button was returned")
	}
}

func TestConfirmDialogCancelled(t *testing.T) {
	f := &fakeOsascript{err: errors.New("exit status 1")}
	if ConfirmDialog(f, "Title", "Message", "Remove") {
		t.Error("ConfirmDialog on cancel/error should be false")
	}
}

func TestAlertSendsScript(t *testing.T) {
	f := &fakeOsascript{}
	Alert(f, "claude-swap", "something went wrong")
	if !strings.Contains(f.lastScript, "something went wrong") {
		t.Errorf("Alert script = %q, want it to contain the message", f.lastScript)
	}
}

func TestNotifySendsScript(t *testing.T) {
	f := &fakeOsascript{}
	Notify(f, "claude-swap", "switched accounts")
	if !strings.Contains(f.lastScript, "switched accounts") || !strings.Contains(f.lastScript, "display notification") {
		t.Errorf("Notify script = %q", f.lastScript)
	}
}
