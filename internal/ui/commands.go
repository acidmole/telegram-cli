package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/acidmole/telegram-cli/internal/telegram"
)

// command is something typed in the input line after a slash. A slash at
// the start of a message always means a command; "//" sends one literally.
type command struct {
	name, args, about string
	run               func(m Model, args string) (tea.Model, tea.Cmd)
}

// commands lists what /help shows, in that order.
var commands []command

func init() {
	commands = []command{
		{"/img", "<path> [caption]", "send an image (Ctrl+V pastes one)", runImg},
		{"/help", "", "show this help (also Ctrl+H)", func(m Model, _ string) (tea.Model, tea.Cmd) {
			m.input.Reset()
			m.openHelp()
			return m, nil
		}},
	}
}

func (m Model) runCommand(text string) (tea.Model, tea.Cmd) {
	name, args, _ := strings.Cut(text, " ")
	for _, c := range commands {
		if c.name == name {
			return c.run(m, strings.TrimSpace(args))
		}
	}
	// Keep the text so the typo can be fixed.
	m.status = fmt.Sprintf("Unknown command %s. /help lists them; // sends a message starting with /.", name)
	return m, nil
}

func runImg(m Model, args string) (tea.Model, tea.Cmd) {
	path, caption, _ := strings.Cut(args, " ")
	if path == "" {
		m.status = "Usage: /img <path> [caption]"
		return m, nil
	}
	replyTo := 0
	if m.replyTo != nil {
		replyTo = m.replyTo.ID
	}
	m.input.Reset()
	m.replyTo = nil
	path = expandHome(path)
	a := attachment{path: path, name: filepath.Base(path)}
	m.status = "Uploading " + telegram.ShortName(a.name) + "..."
	return m, m.sendImage(*m.open, a, strings.TrimSpace(caption), replyTo)
}

func (m *Model) openHelp() {
	m.help = true
	m.helpOffset = m.view.YOffset
	m.render()
	m.view.GotoTop()
}

func (m *Model) closeHelp() {
	m.help = false
	m.render()
	m.view.SetYOffset(m.helpOffset)
}

func helpText() string {
	var b strings.Builder
	section := func(title string, rows [][2]string) {
		b.WriteString(bold.Render(title) + "\n")
		for _, r := range rows {
			fmt.Fprintf(&b, "  %-24s %s\n", r[0], r[1])
		}
		b.WriteString("\n")
	}

	cmds := make([][2]string, 0, len(commands)+1)
	for _, c := range commands {
		cmds = append(cmds, [2]string{strings.TrimSpace(c.name + " " + c.args), c.about})
	}
	cmds = append(cmds, [2]string{"//text", "send text that starts with /"})

	section("Commands", cmds)
	section("Chat list", [][2]string{
		{"Up/Down, j/k", "move"},
		{"Enter", "open the chat"},
		{"/", "search chats by name, Esc clears"},
		{"q", "quit"},
	})
	section("Chat", [][2]string{
		{"Enter", "send"},
		{"Ctrl+R", "reply to a message"},
		{"Ctrl+V", "paste text or an image"},
		{"PgUp/PgDn", "scroll; PgUp at the top loads older"},
		{"Esc", "drop image or reply, then back to the list"},
	})
	section("Anywhere", [][2]string{
		{"Tab", "switch between list and chat"},
		{"Ctrl+H", "this help"},
		{"Ctrl+C", "quit"},
	})
	return b.String()
}
