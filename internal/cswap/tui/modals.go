// internal/cswap/tui/modals.go
package tui

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

// popScreenMsg asks app.go's root model to pop the current screen.
type popScreenMsg struct{}

func popScreen() tea.Cmd {
	return func() tea.Msg { return popScreenMsg{} }
}

// ConfirmModal mirrors ConfirmModal: yes/no confirmation. y/n answer
// directly regardless of focus; left/right move focus between the two
// buttons and enter presses whichever is focused (mirrors Textual's
// Binding("left", "app.focus_previous") / Binding("right", "app.focus_next")
// plus Button's own on_button_pressed, rather than enter always confirming).
type ConfirmModal struct {
	message, title, yesLabel string
	focus                    int // 0=yes, 1=cancel
	onDismiss                func(confirmed bool)
}

func NewConfirmModal(message, title, yesLabel string, onDismiss func(confirmed bool)) *ConfirmModal {
	return &ConfirmModal{message: message, title: title, yesLabel: yesLabel, onDismiss: onDismiss}
}

func (m *ConfirmModal) Init() tea.Cmd { return nil }

func (m *ConfirmModal) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch keyMsg.String() {
	case "y":
		m.onDismiss(true)
		return m, popScreen()
	case "n", "esc":
		m.onDismiss(false)
		return m, popScreen()
	case "left", "right":
		m.focus = 1 - m.focus
		return m, nil
	case "enter":
		m.onDismiss(m.focus == 0)
		return m, popScreen()
	}
	return m, nil
}

func (m *ConfirmModal) View() string {
	var b strings.Builder
	b.WriteString(StyleBold.Render(m.title))
	b.WriteString("\n\n")
	b.WriteString(m.message)
	b.WriteString("\n\n")
	b.WriteString(confirmButtonText(m.yesLabel, m.focus == 0))
	b.WriteString("  ")
	b.WriteString(confirmButtonText("Cancel", m.focus == 1))
	b.WriteString("\n\n")
	b.WriteString(StyleMuted.Render("← → · enter  ·  y " + strings.ToLower(m.yesLabel) + "  ·  n / esc cancel"))
	return b.String()
}

func confirmButtonText(label string, focused bool) string {
	text := "[ " + label + " ]"
	if focused {
		return StyleBold.Render(text)
	}
	return StyleMuted.Render(text)
}

// TokenForm mirrors TokenForm: what the add-token modal collects.
type TokenForm struct {
	Token string
	Email string
	Slot  *int
}

// AddTokenModal mirrors AddTokenModal: collects a token, optional email,
// optional slot.
type AddTokenModal struct {
	tokenInput, emailInput, slotInput textinput.Model
	formError                         string
	focus                             int // 0=token 1=email 2=slot
	onDismiss                         func(form *TokenForm)
}

func NewAddTokenModal(onDismiss func(form *TokenForm)) *AddTokenModal {
	token := textinput.New()
	token.Placeholder = "token (required)"
	token.EchoMode = textinput.EchoPassword
	token.Focus()

	email := textinput.New()
	email.Placeholder = "email label (optional)"

	slot := textinput.New()
	slot.Placeholder = "slot number (optional)"

	return &AddTokenModal{tokenInput: token, emailInput: email, slotInput: slot, onDismiss: onDismiss}
}

func (m *AddTokenModal) Init() tea.Cmd { return textinput.Blink }

func (m *AddTokenModal) inputs() []*textinput.Model {
	return []*textinput.Model{&m.tokenInput, &m.emailInput, &m.slotInput}
}

func (m *AddTokenModal) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, isKey := msg.(tea.KeyMsg)
	if isKey {
		switch keyMsg.String() {
		case "esc":
			m.onDismiss(nil)
			return m, popScreen()
		case "tab":
			m.inputs()[m.focus].Blur()
			m.focus = (m.focus + 1) % 3
			m.inputs()[m.focus].Focus()
			return m, nil
		case "enter":
			return m, m.submit()
		}
		// Mirrors Input(..., type="integer"): Textual rejects non-digit
		// keystrokes on the slot field as they're typed, not just at submit
		// time. bubbles/textinput has no built-in char-class restriction (its
		// Validate hook only flags m.Err, it doesn't stop the rune from being
		// inserted), so the slot field's own keystrokes are filtered here
		// before ever reaching textinput.Update.
		if m.focus == 2 && keyMsg.Type == tea.KeyRunes {
			for _, r := range keyMsg.Runes {
				if r < '0' || r > '9' {
					return m, nil
				}
			}
		}
	}
	var cmd tea.Cmd
	*m.inputs()[m.focus], cmd = m.inputs()[m.focus].Update(msg)
	return m, cmd
}

func (m *AddTokenModal) submit() tea.Cmd {
	token := strings.TrimSpace(m.tokenInput.Value())
	if token == "" {
		m.formError = "Token is required."
		return nil
	}
	var slot *int
	if raw := strings.TrimSpace(m.slotInput.Value()); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			m.formError = "Slot must be a number."
			return nil
		}
		if n < 1 {
			m.formError = "Slot must be >= 1."
			return nil
		}
		slot = &n
	}
	m.formError = ""
	form := &TokenForm{Token: token, Email: strings.TrimSpace(m.emailInput.Value()), Slot: slot}
	m.onDismiss(form)
	return popScreen()
}

func (m *AddTokenModal) View() string {
	var b strings.Builder
	b.WriteString(StyleBold.Render("Add account from token"))
	b.WriteString("\n\n")
	b.WriteString(m.tokenInput.View())
	b.WriteString("\n")
	b.WriteString(m.emailInput.View())
	b.WriteString("\n")
	b.WriteString(m.slotInput.View())
	if m.formError != "" {
		b.WriteString("\n")
		b.WriteString(m.formError)
	}
	b.WriteString("\n\n")
	b.WriteString(StyleMuted.Render("enter add  ·  tab next field  ·  esc cancel"))
	return b.String()
}

// OutputModal mirrors OutputModal: scrollable display of one action's
// output (its message on success, or the error text on failure — see
// data.go's ActionResult, which has no separate ANSI-captured stream the
// way Python's does).
type OutputModal struct {
	title     string
	viewport  viewport.Model
	onDismiss func()
}

func NewOutputModal(title, output string, onDismiss func()) *OutputModal {
	vp := viewport.New(76, 16)
	if strings.TrimSpace(output) == "" {
		output = "(no output)"
	}
	vp.SetContent(output)
	return &OutputModal{title: title, viewport: vp, onDismiss: onDismiss}
}

func (m *OutputModal) Init() tea.Cmd { return nil }

func (m *OutputModal) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "esc", "q", "enter":
			m.onDismiss()
			return m, popScreen()
		}
	}
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m *OutputModal) View() string {
	var b strings.Builder
	b.WriteString(StyleBold.Render(m.title))
	b.WriteString("\n\n")
	b.WriteString(m.viewport.View())
	b.WriteString("\n\n")
	b.WriteString(StyleMuted.Render("esc close"))
	return b.String()
}
