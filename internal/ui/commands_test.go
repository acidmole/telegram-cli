package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func typeAndEnter(t *testing.T, m Model, text string) (Model, tea.Cmd) {
	t.Helper()
	model, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = model.(Model)
	m.setFocus(focusInput)
	m.input.SetValue(text)
	model, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	return model.(Model), cmd
}

func TestUnknownCommandIsNotSent(t *testing.T) {
	m, cmd := typeAndEnter(t, openModel(t, "u1"), "/imgg cat.png")
	if cmd != nil {
		t.Error("an unknown command was sent")
	}
	if m.input.Value() != "/imgg cat.png" {
		t.Errorf("input is %q, want the mistyped command kept", m.input.Value())
	}
	if !strings.Contains(m.status, "Unknown command /imgg") {
		t.Errorf("status is %q", m.status)
	}
}

func TestDoubleSlashSendsText(t *testing.T) {
	m, cmd := typeAndEnter(t, openModel(t, "u1"), "//start")
	if cmd == nil || m.input.Value() != "" {
		t.Errorf("//start was not sent")
	}
}

func TestHelp(t *testing.T) {
	m, _ := typeAndEnter(t, openModel(t, "u1"), "/help")
	if !m.help || !strings.Contains(m.view.View(), "/img <path> [caption]") {
		t.Fatal("/help did not show the command list")
	}

	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	m = model.(Model)
	if m.help || m.input.Value() != "" {
		t.Errorf("a key did not just close the help: help=%v input=%q", m.help, m.input.Value())
	}

	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlH})
	if !model.(Model).help {
		t.Error("Ctrl+H did not open the help")
	}
}
