package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestConfirmModalYConfirms(t *testing.T) {
	var got *bool
	m := NewConfirmModal("Remove account 2?", "Remove account", "Remove", func(confirmed bool) {
		got = &confirmed
	})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if got == nil || !*got {
		t.Fatalf("got %v, want confirmed=true", got)
	}
	if cmd == nil {
		t.Fatal("expected a pop-screen command after dismissal")
	}
}

func TestConfirmModalEscCancels(t *testing.T) {
	var got *bool
	m := NewConfirmModal("Remove account 2?", "Remove account", "Remove", func(confirmed bool) {
		got = &confirmed
	})
	m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	if got == nil || *got {
		t.Fatalf("got %v, want confirmed=false", got)
	}
}

// TestConfirmModalLeftRightTogglesFocusAndEnterRespectsIt is the regression
// test for a real bug: an earlier draft treated "enter" as always-confirm,
// ignoring focus entirely. Python's ConfirmModal moves focus between its Yes
// and Cancel buttons with left/right (Binding("left", "app.focus_previous"),
// Binding("right", "app.focus_next")) and enter presses whichever button is
// focused, via Button's own on_button_pressed.
func TestConfirmModalLeftRightTogglesFocusAndEnterRespectsIt(t *testing.T) {
	var got *bool
	m := NewConfirmModal("Remove account 2?", "Remove account", "Remove", func(confirmed bool) {
		got = &confirmed
	})
	m.Update(tea.KeyMsg{Type: tea.KeyRight})
	if m.focus != 1 {
		t.Fatalf("focus after right = %d, want 1 (cancel)", m.focus)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if got == nil || *got {
		t.Fatalf("got %v, want confirmed=false after right then enter", got)
	}
}

// TestConfirmModalYNKeysBypassFocus proves y/n still answer directly
// regardless of which button is focused, matching Python's Binding("y", ...)
// / Binding("n,escape", ...), which fire independently of Button focus.
func TestConfirmModalYNKeysBypassFocus(t *testing.T) {
	var got *bool
	m := NewConfirmModal("Remove account 2?", "Remove account", "Remove", func(confirmed bool) {
		got = &confirmed
	})
	m.Update(tea.KeyMsg{Type: tea.KeyRight}) // focus lands on cancel
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if got == nil || !*got {
		t.Fatalf("got %v, want confirmed=true (y bypasses focus)", got)
	}
}

func TestAddTokenModalRequiresToken(t *testing.T) {
	var got *TokenForm
	m := NewAddTokenModal(func(form *TokenForm) { got = form })
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if got != nil {
		t.Fatalf("empty token must not submit, got %+v", got)
	}
	if m.formError == "" {
		t.Error("expected a form error for the empty token")
	}
}

func TestAddTokenModalSubmitsValidForm(t *testing.T) {
	var got *TokenForm
	m := NewAddTokenModal(func(form *TokenForm) { got = form })
	m.tokenInput.SetValue("sk-ant-oat-example")
	m.emailInput.SetValue("me@example.com")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if got == nil || got.Token != "sk-ant-oat-example" || got.Email != "me@example.com" {
		t.Fatalf("got %+v", got)
	}
}

func TestAddTokenModalEscCancelsWithNilForm(t *testing.T) {
	called := false
	var got *TokenForm = &TokenForm{} // sentinel to prove the callback overwrites it
	m := NewAddTokenModal(func(form *TokenForm) { called = true; got = form })
	m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	if !called || got != nil {
		t.Fatalf("called=%v got=%+v, want called=true got=nil", called, got)
	}
}

func TestAddTokenModalClearsFormErrorOnSuccessfulSubmit(t *testing.T) {
	var got *TokenForm
	m := NewAddTokenModal(func(form *TokenForm) { got = form })
	// First, submit with empty token to set formError
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.formError == "" {
		t.Fatal("expected formError to be set after empty token submit")
	}
	// Now set a valid token and email
	m.tokenInput.SetValue("sk-ant-oat-example")
	m.emailInput.SetValue("me@example.com")
	// Submit successfully
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	// Assert formError was cleared and form was submitted
	if m.formError != "" {
		t.Errorf("expected formError to be cleared after successful submit, got %q", m.formError)
	}
	if got == nil || got.Token != "sk-ant-oat-example" {
		t.Fatalf("expected successful form submission, got %+v", got)
	}
}

// TestAddTokenModalSlotFieldRejectsNonDigitKeystrokes is the regression test
// for a real gap: Python's slot Input uses type="integer", which rejects
// non-digit characters as they're typed (not just at submit). bubbles'
// textinput.Validate only flags Model.Err after the fact, it never blocks
// the rune from being inserted, so the filter has to live in Update itself.
func TestAddTokenModalSlotFieldRejectsNonDigitKeystrokes(t *testing.T) {
	m := NewAddTokenModal(func(form *TokenForm) {})
	m.Update(tea.KeyMsg{Type: tea.KeyTab}) // token -> email
	m.Update(tea.KeyMsg{Type: tea.KeyTab}) // email -> slot
	if m.focus != 2 {
		t.Fatalf("focus = %d, want 2 (slot)", m.focus)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	if m.slotInput.Value() != "" {
		t.Fatalf("slotInput.Value() = %q after a letter keystroke, want it rejected", m.slotInput.Value())
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("3")})
	if m.slotInput.Value() != "3" {
		t.Fatalf("slotInput.Value() = %q after a digit keystroke, want %q", m.slotInput.Value(), "3")
	}
}

func TestOutputModalAnyDismissKeyCloses(t *testing.T) {
	called := false
	m := NewOutputModal("Add account, failed", "Error: no active claude account found", func() { called = true })
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !called {
		t.Error("expected the dismiss callback to fire")
	}
}
