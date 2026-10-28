// Package ui is the Bubble Tea terminal interface: a chat list on the left,
// the open chat on the right and an input line at the bottom.
package ui

import (
	"context"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"telegram-cli/internal/telegram"
)

const listWidth = 30

var (
	asciiBorder = lipgloss.Border{
		Top: "-", Bottom: "-", Left: "|", Right: "|",
		TopLeft: "+", TopRight: "+", BottomLeft: "+", BottomRight: "+",
	}
	pane       = lipgloss.NewStyle().Border(asciiBorder)
	activePane = pane.BorderForeground(lipgloss.Color("12"))
	dim        = lipgloss.NewStyle().Faint(true)
	bold       = lipgloss.NewStyle().Bold(true)
	picked     = lipgloss.NewStyle().Reverse(true)

	// nameColors are 256-color codes that stay readable on dark and light
	// backgrounds; each sender keeps one of them for good.
	nameColors = []lipgloss.Color{"167", "173", "179", "107", "72", "74", "68", "134", "169", "142", "37", "98"}
	meStyle    = bold.Foreground(lipgloss.Color("245"))
)

type focus int

const (
	focusList focus = iota
	focusInput
	// focusPick selects the message to reply to.
	focusPick
)

type (
	chatsMsg   []telegram.Chat
	historyMsg struct {
		key   string
		older bool
		msgs  []telegram.Message
	}
	incomingMsg telegram.Message
	sentMsg     struct{ chat telegram.Chat }
	errMsg      struct{ err error }
)

// Options tune the interface.
type Options struct {
	// Notify sends a desktop notification for messages in chats that are
	// neither open nor muted.
	Notify bool
}

// Model is the root Bubble Tea model.
type Model struct {
	ctx    context.Context
	client *telegram.Client
	opts   Options

	chats    []telegram.Chat
	cursor   int
	open     *telegram.Chat
	messages []telegram.Message
	// lineStarts[i] is the viewport line where messages[i] begins.
	lineStarts []int

	loadingOlder bool
	noOlder      bool

	focus   focus
	pick    int
	replyTo *telegram.Message

	view   viewport.Model
	input  textinput.Model
	status string

	width, height int
}

// New builds the interface on top of an attached client.
func New(ctx context.Context, client *telegram.Client, opts Options) Model {
	in := textinput.New()
	in.Placeholder = "Type a message, Enter to send"
	in.Prompt = "> "
	return Model{
		ctx:    ctx,
		client: client,
		opts:   opts,
		input:  in,
		view:   viewport.New(0, 0),
		status: "Loading chats...",
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.loadChats(), m.waitForUpdate())
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		return m, nil

	case chatsMsg:
		m.chats = msg
		if m.cursor >= len(m.chats) {
			m.cursor = max(len(m.chats)-1, 0)
		}
		m.status = fmt.Sprintf("%d chats. Enter: open  Tab: switch pane  Esc: back  Ctrl+C: quit", len(m.chats))
		return m, nil

	case historyMsg:
		if m.open == nil || m.open.Key != msg.key {
			return m, nil
		}
		if !msg.older {
			m.messages = msg.msgs
			m.noOlder = len(msg.msgs) < telegram.HistoryPage
			m.render()
			m.view.GotoBottom()
			return m, m.markRead(msg.msgs)
		}
		m.loadingOlder = false
		m.noOlder = len(msg.msgs) < telegram.HistoryPage
		// Keep the lines on screen where they were when older ones arrive.
		before := m.view.TotalLineCount()
		m.messages = append(msg.msgs, m.messages...)
		m.pick += len(msg.msgs)
		m.render()
		m.view.SetYOffset(m.view.YOffset + m.view.TotalLineCount() - before)
		return m, nil

	case incomingMsg:
		return m, tea.Batch(m.receive(telegram.Message(msg)), m.waitForUpdate())

	case sentMsg:
		if strings.HasPrefix(m.status, "Uploading ") {
			m.status = "Sent."
		}
		return m, m.loadHistory(msg.chat)

	case errMsg:
		m.status = "Error: " + msg.err.Error()
		m.loadingOlder = false
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) handleKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "tab":
		if m.open != nil && m.focus != focusPick {
			m.setFocus(focusList + focusInput - m.focus)
		}
		return m, nil
	case "pgup", "pgdown":
		if m.focus == focusPick {
			break
		}
		var cmd tea.Cmd
		m.view, cmd = m.view.Update(k)
		if k.String() == "pgup" && m.view.AtTop() {
			cmd = tea.Batch(cmd, m.loadOlder())
		}
		return m, cmd
	}

	switch m.focus {
	case focusInput:
		return m.handleInputKey(k)
	case focusPick:
		return m.handlePickKey(k)
	}

	switch k.String() {
	case "q":
		return m, tea.Quit
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.chats)-1 {
			m.cursor++
		}
	case "enter", "l":
		if len(m.chats) == 0 {
			return m, nil
		}
		m.chats[m.cursor].Unread = 0
		chat := m.chats[m.cursor]
		m.open = &chat
		m.messages = nil
		m.replyTo = nil
		m.noOlder, m.loadingOlder = false, false
		m.render()
		m.setFocus(focusInput)
		return m, m.loadHistory(chat)
	}
	return m, nil
}

func (m Model) handleInputKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "esc":
		if m.replyTo != nil {
			m.replyTo = nil
		} else {
			m.setFocus(focusList)
		}
		return m, nil
	case "ctrl+r":
		if len(m.messages) > 0 {
			m.pick = len(m.messages) - 1
			m.setFocus(focusPick)
			m.render()
			m.showPicked()
		}
		return m, nil
	case "enter":
		text := strings.TrimSpace(m.input.Value())
		if text == "" || m.open == nil {
			return m, nil
		}
		replyTo := 0
		if m.replyTo != nil {
			replyTo = m.replyTo.ID
		}
		m.input.Reset()
		m.replyTo = nil
		if rest, ok := strings.CutPrefix(text, "/img "); ok {
			path, caption, _ := strings.Cut(strings.TrimSpace(rest), " ")
			m.status = "Uploading " + telegram.ShortName(filepath.Base(path)) + "..."
			return m, m.sendImage(*m.open, expandHome(path), strings.TrimSpace(caption), replyTo)
		}
		return m, m.send(*m.open, text, replyTo)
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(k)
	return m, cmd
}

func (m Model) handlePickKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch k.String() {
	case "up", "k":
		if m.pick > 0 {
			m.pick--
		} else {
			cmd = m.loadOlder()
		}
	case "down", "j":
		if m.pick < len(m.messages)-1 {
			m.pick++
		}
	case "enter":
		msg := m.messages[m.pick]
		m.replyTo = &msg
		m.setFocus(focusInput)
	case "esc", "ctrl+r":
		m.setFocus(focusInput)
	default:
		return m, nil
	}
	m.render()
	if m.focus == focusPick {
		m.showPicked()
	}
	return m, cmd
}

// receive appends a message to the open chat, or bumps the unread counter of
// the chat it belongs to and moves that chat to the top.
func (m *Model) receive(msg telegram.Message) tea.Cmd {
	var cmds []tea.Cmd
	if m.open != nil && m.open.Key == msg.ChatKey {
		follow := m.view.AtBottom()
		m.messages = append(m.messages, msg)
		m.render()
		if follow {
			m.view.GotoBottom()
		}
		cmds = append(cmds, m.markRead([]telegram.Message{msg}))
	}

	for i, c := range m.chats {
		if c.Key != msg.ChatKey {
			continue
		}
		if (m.open == nil || m.open.Key != c.Key) && !msg.Out {
			c.Unread++
			if m.opts.Notify && !c.Muted {
				cmds = append(cmds, notify(c, msg))
			}
		}
		selected := m.chats[m.cursor].Key
		copy(m.chats[1:i+1], m.chats[:i])
		m.chats[0] = c
		for j := range m.chats {
			if m.chats[j].Key == selected {
				m.cursor = j
			}
		}
		return tea.Batch(cmds...)
	}
	// A chat we have not listed yet: refresh the list.
	return tea.Batch(append(cmds, m.loadChats())...)
}

func (m *Model) setFocus(f focus) {
	m.focus = f
	if f == focusInput {
		m.input.Focus()
	} else {
		m.input.Blur()
	}
	m.render()
}

func (m *Model) layout() {
	// Messages pane: two border rows. Input pane: hint line, input line and
	// two border rows. One status line under both.
	m.view.Width = max(m.width-listWidth-4, 10)
	m.view.Height = max(m.height-2-4-1, 3)
	m.input.Width = m.view.Width - 3
	m.render()
}

// render rebuilds the message pane content. It leaves the scroll position to
// the caller.
func (m *Model) render() {
	if m.open == nil {
		m.view.SetContent(dim.Render("Select a chat and press Enter."))
		return
	}
	byID := make(map[int]telegram.Message, len(m.messages))
	for _, msg := range m.messages {
		byID[msg.ID] = msg
	}

	wrap := lipgloss.NewStyle().Width(m.view.Width)
	var b strings.Builder
	line := 0
	m.lineStarts = m.lineStarts[:0]
	for i, msg := range m.messages {
		m.lineStarts = append(m.lineStarts, line)
		var block strings.Builder
		if msg.ReplyTo != 0 {
			quote := "| re: an earlier message"
			if orig, ok := byID[msg.ReplyTo]; ok {
				quote = "| re " + senderName(orig) + ": " + oneLine(orig.Text)
			}
			block.WriteString(dim.Render(truncate(quote, m.view.Width)) + "\n")
		}
		head := dim.Render(msg.Date.Format("Jan 02 15:04")) + " " + nameStyle(msg).Render(senderName(msg))
		block.WriteString(wrap.Render(head + ": " + msg.Text))

		text := block.String()
		if m.focus == focusPick && i == m.pick {
			text = picked.Render(text)
		}
		b.WriteString(text + "\n")
		line += strings.Count(text, "\n") + 1
	}
	m.view.SetContent(b.String())
}

// showPicked scrolls just enough to keep the picked message on screen.
func (m *Model) showPicked() {
	if m.pick >= len(m.lineStarts) {
		return
	}
	top := m.lineStarts[m.pick]
	bottom := m.view.TotalLineCount()
	if m.pick+1 < len(m.lineStarts) {
		bottom = m.lineStarts[m.pick+1]
	}
	if top < m.view.YOffset {
		m.view.SetYOffset(top)
	} else if bottom > m.view.YOffset+m.view.Height {
		m.view.SetYOffset(bottom - m.view.Height)
	}
}

func (m Model) View() string {
	if m.width == 0 {
		return ""
	}
	listStyle, chatStyle := activePane, pane
	if m.focus != focusList {
		listStyle, chatStyle = pane, activePane
	}

	list := listStyle.Width(listWidth).Height(m.view.Height + 4).Render(m.renderList(m.view.Height + 4))

	hint := dim.Render(truncate("Ctrl+R: reply to a message  /img <path> [caption]: attach an image", m.view.Width))
	switch {
	case m.focus == focusPick:
		hint = dim.Render("Up/Down: choose  Enter: reply  Esc: cancel")
	case m.replyTo != nil:
		hint = truncate("Replying to "+senderName(*m.replyTo)+": "+oneLine(m.replyTo.Text), m.view.Width-14) + dim.Render(" (Esc cancels)")
	}
	chat := lipgloss.JoinVertical(lipgloss.Left,
		chatStyle.Width(m.view.Width).Height(m.view.Height).Render(m.view.View()),
		chatStyle.Width(m.view.Width).Render(hint+"\n"+m.input.View()),
	)

	title := "No chat open"
	if m.open != nil {
		title = m.open.Title
	}
	status := m.status
	if m.loadingOlder {
		status = "Loading older messages..."
	}
	body := lipgloss.JoinHorizontal(lipgloss.Top, list, chat)
	return body + "\n" + dim.Render(truncate(" "+title+"  |  "+status, m.width))
}

func (m Model) renderList(height int) string {
	if len(m.chats) == 0 {
		return dim.Render("(no chats)")
	}
	// Keep the cursor visible by scrolling the window of rows.
	start := 0
	if m.cursor >= height {
		start = m.cursor - height + 1
	}
	var rows []string
	for i := start; i < len(m.chats) && i < start+height; i++ {
		c := m.chats[i]
		marker := "  "
		if i == m.cursor {
			marker = "> "
		}
		unread := ""
		if c.Unread > 0 {
			unread = fmt.Sprintf(" (%d)", c.Unread)
		}
		row := truncate(marker+c.Title, listWidth-len(unread)) + unread
		if i == m.cursor {
			row = bold.Render(row)
		}
		rows = append(rows, row)
	}
	return strings.Join(rows, "\n")
}

func (m Model) loadChats() tea.Cmd {
	return func() tea.Msg {
		chats, err := m.client.Chats(m.ctx)
		if err != nil {
			return errMsg{err}
		}
		return chatsMsg(chats)
	}
}

func (m Model) loadHistory(chat telegram.Chat) tea.Cmd {
	return func() tea.Msg {
		msgs, err := m.client.History(m.ctx, chat, 0)
		if err != nil {
			return errMsg{err}
		}
		return historyMsg{key: chat.Key, msgs: msgs}
	}
}

// loadOlder fetches the page before the oldest loaded message, unless one is
// already on its way or the chat has no more.
func (m *Model) loadOlder() tea.Cmd {
	if m.open == nil || len(m.messages) == 0 || m.loadingOlder || m.noOlder {
		return nil
	}
	m.loadingOlder = true
	chat, oldest := *m.open, m.messages[0].ID
	return func() tea.Msg {
		msgs, err := m.client.History(m.ctx, chat, oldest)
		if err != nil {
			return errMsg{err}
		}
		return historyMsg{key: chat.Key, older: true, msgs: msgs}
	}
}

func (m Model) markRead(msgs []telegram.Message) tea.Cmd {
	if m.open == nil || len(msgs) == 0 {
		return nil
	}
	chat, maxID := *m.open, msgs[len(msgs)-1].ID
	return func() tea.Msg {
		if err := m.client.MarkRead(m.ctx, chat, maxID); err != nil {
			return errMsg{err}
		}
		return nil
	}
}

func (m Model) send(chat telegram.Chat, text string, replyTo int) tea.Cmd {
	return func() tea.Msg {
		if err := m.client.Send(m.ctx, chat, text, replyTo); err != nil {
			return errMsg{err}
		}
		return sentMsg{chat}
	}
}

func (m Model) sendImage(chat telegram.Chat, path, caption string, replyTo int) tea.Cmd {
	return func() tea.Msg {
		if err := m.client.SendImage(m.ctx, chat, path, caption, replyTo); err != nil {
			return errMsg{err}
		}
		return sentMsg{chat}
	}
}

func (m Model) waitForUpdate() tea.Cmd {
	return func() tea.Msg {
		select {
		case msg := <-m.client.Updates():
			return incomingMsg(msg)
		case <-m.ctx.Done():
			return nil
		}
	}
}

// expandHome turns a leading ~/ into the home directory, as a shell would.
func expandHome(path string) string {
	if rest, ok := strings.CutPrefix(path, "~/"); ok {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, rest)
		}
	}
	return path
}

func senderName(msg telegram.Message) string {
	if msg.Out {
		return "me"
	}
	return msg.From
}

func nameStyle(msg telegram.Message) lipgloss.Style {
	if msg.Out {
		return meStyle
	}
	h := fnv.New32a()
	h.Write([]byte(msg.FromKey))
	return bold.Foreground(nameColors[h.Sum32()%uint32(len(nameColors))])
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func truncate(s string, width int) string {
	r := []rune(s)
	if len(r) <= width {
		return s
	}
	if width <= 3 {
		return string(r[:max(width, 0)])
	}
	return string(r[:width-3]) + "..."
}
