package ui

import (
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"telegram-cli/internal/telegram"
)

// notify shows a desktop notification through notify-send. Failures are
// ignored: a missing notification daemon must not disturb the chat.
func notify(chat telegram.Chat, msg telegram.Message) tea.Cmd {
	body := oneLine(msg.Text)
	// In groups and channels, say who wrote it.
	if msg.FromKey != msg.ChatKey {
		body = msg.From + ": " + body
	}
	if r := []rune(body); len(r) > 200 {
		body = string(r[:197]) + "..."
	}
	return func() tea.Msg {
		_ = exec.Command("notify-send", "--app-name=telegram-cli", "--", chat.Title, strings.TrimSpace(body)).Run()
		return nil
	}
}
