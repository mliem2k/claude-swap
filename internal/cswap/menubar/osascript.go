package menubar

import (
	"os/exec"
	"strings"
)

// osascriptRunner abstracts running one AppleScript via osascript, the
// same fake-backed-interface convention macos_keychain.go/
// macos_keychain_test.go already use for /usr/bin/security.
type osascriptRunner interface {
	run(script string) (stdout string, err error)
}

// realOSAScript shells out to the real osascript binary.
type realOSAScript struct{}

func (realOSAScript) run(script string) (string, error) {
	out, err := exec.Command("osascript", "-e", script).Output()
	return string(out), err
}

func quoteAppleScript(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

// TextInputDialog mirrors the two rumps.Window(...) text-input prompts
// (email, then setup-token) in Python's on_add_token. ok=false on
// cancel, an osascript error, or an empty answer.
func TextInputDialog(r osascriptRunner, title, message string) (string, bool) {
	script := `display dialog ` + quoteAppleScript(message) +
		` with title ` + quoteAppleScript(title) +
		` default answer "" buttons {"Cancel", "Next"} default button "Next"`
	out, err := r.run(script)
	if err != nil {
		return "", false
	}
	idx := strings.Index(out, "text returned:")
	if idx < 0 {
		return "", false
	}
	value := strings.TrimSpace(out[idx+len("text returned:"):])
	if value == "" {
		return "", false
	}
	return value, true
}

// ConfirmDialog mirrors the remove-account rumps.alert(...) confirm.
// True only when confirmLabel was the button actually clicked.
func ConfirmDialog(r osascriptRunner, title, message, confirmLabel string) bool {
	script := `display dialog ` + quoteAppleScript(message) +
		` with title ` + quoteAppleScript(title) +
		` buttons {"Cancel", ` + quoteAppleScript(confirmLabel) + `} default button ` + quoteAppleScript(confirmLabel)
	out, err := r.run(script)
	if err != nil {
		return false
	}
	return strings.Contains(out, "button returned:"+confirmLabel)
}

// Alert mirrors rumps.alert(title=..., message=...): a modal error/info
// alert with a single OK button.
func Alert(r osascriptRunner, title, message string) {
	script := `display alert ` + quoteAppleScript(title) + ` message ` + quoteAppleScript(message)
	_, _ = r.run(script)
}

// Notify mirrors rumps.notification(title, subtitle, message): here
// Python always passes "claude-swap" as the title and the real subtitle
// as its second arg, collapsed here into a single (title, message) pair
// matching how every call site in this port actually uses it.
func Notify(r osascriptRunner, title, message string) {
	script := `display notification ` + quoteAppleScript(message) + ` with title ` + quoteAppleScript(title)
	_, _ = r.run(script)
}

// RevealInFinder mirrors on_open_log's `open -R <path>` call directly,
// no osascript involved.
func RevealInFinder(path string) error {
	return exec.Command("open", "-R", path).Run()
}
