// Package ui is the Bubble Tea terminal interface: a chat list on the left,
// the open chat on the right and an input line at the bottom.
package ui

import (
	"context"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

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
	// mine colors your own messages, name and text alike: a quiet steel
	// blue, darker on light backgrounds.
	mine    = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "24", Dark: "110"})
	meStyle = mine.Bold(true)
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
	eventMsg   struct{ ev telegram.Event }
	typingTick struct{}
	sentMsg    struct{ chat telegram.Chat }
	errMsg     struct{ err error }
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

	chats []telegram.Chat
	// cursor indexes the chats the search leaves visible.
	cursor    int
	searching bool
	search    textinput.Model
	open      *telegram.Chat
	messages  []telegram.Message
	// lineStarts[i] is the viewport line where messages[i] begins.
	lineStarts []int

	loadingOlder bool
	noOlder      bool

	// typing maps a chat key to who is doing what in it, until when.
	typing  map[string]map[string]typer
	ticking bool

	focus focus
	// help shows the key and command reference in the message pane;
	// helpOffset is the scroll position to return to.
	help       bool
	helpOffset int
	pick       int
	replyTo    *telegram.Message
	// attached is a pasted image that goes out with the next Enter.
	attached *attachment

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
	search := textinput.New()
	search.Prompt = "/"
	search.Placeholder = "search chats"
	return Model{
		ctx:    ctx,
		client: client,
		opts:   opts,
		input:  in,
		search: search,
		typing: map[string]map[string]typer{},
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
		if n := len(m.visible()); m.cursor >= n {
			m.cursor = max(n-1, 0)
		}
		m.status = fmt.Sprintf("%d chats. Ctrl+H: help", len(m.chats))
		return m, nil

	case historyMsg:
		if m.open == nil || m.open.Key != msg.key {
			return m, nil
		}
		if !msg.older {
			if len(m.messages) == 0 {
				m.noOlder = len(msg.msgs) < telegram.HistoryPage
			}
			m.messages = mergeLatest(m.messages, msg.msgs)
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

	case eventMsg:
		return m, tea.Batch(m.handleEvent(msg.ev), m.waitForUpdate())

	case typingTick:
		m.ticking = false
		m.pruneTyping(time.Now())
		return m, m.tickTyping()

	case clipboardMsg:
		if msg.image != nil {
			if m.open == nil {
				discard(msg.image)
				return m, nil
			}
			m.attach(msg.image)
			return m, nil
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(msg.text), Paste: true})
		return m, cmd

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
	case "ctrl+h":
		if !m.help {
			m.openHelp()
			return m, nil
		}
	}
	// Any key closes the help, and does nothing else.
	if m.help {
		m.closeHelp()
		return m, nil
	}

	switch k.String() {
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
	if m.searching {
		return m.handleSearchKey(k)
	}

	switch k.String() {
	case "q":
		return m, tea.Quit
	case "/":
		m.searching = true
		m.cursor = 0
		return m, m.search.Focus()
	case "up", "k":
		m.moveCursor(-1)
	case "down", "j":
		m.moveCursor(1)
	case "enter", "l":
		return m.openSelected()
	}
	return m, nil
}

// handleSearchKey edits the chat search. Arrows move through the matches,
// since letters belong to the query.
func (m Model) handleSearchKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "esc":
		m.endSearch()
		return m, nil
	case "up", "ctrl+p":
		m.moveCursor(-1)
		return m, nil
	case "down", "ctrl+n":
		m.moveCursor(1)
		return m, nil
	case "enter":
		return m.openSelected()
	}
	var cmd tea.Cmd
	before := m.search.Value()
	m.search, cmd = m.search.Update(k)
	if m.search.Value() != before {
		m.cursor = 0
	}
	return m, cmd
}

// endSearch clears the search and keeps the chat it had selected selected.
func (m *Model) endSearch() {
	key, ok := m.selectedKey()
	m.searching = false
	m.search.Reset()
	m.search.Blur()
	m.cursor = 0
	if ok {
		m.selectKey(key)
	}
}

// visible returns the indexes in m.chats of the chats the search matches,
// all of them when there is no search.
func (m Model) visible() []int {
	query := strings.ToLower(strings.TrimSpace(m.search.Value()))
	idx := make([]int, 0, len(m.chats))
	for i, c := range m.chats {
		if !m.searching || query == "" || strings.Contains(strings.ToLower(c.Title), query) {
			idx = append(idx, i)
		}
	}
	return idx
}

func (m Model) selectedKey() (string, bool) {
	vis := m.visible()
	if m.cursor >= len(vis) {
		return "", false
	}
	return m.chats[vis[m.cursor]].Key, true
}

func (m *Model) selectKey(key string) {
	for j, i := range m.visible() {
		if m.chats[i].Key == key {
			m.cursor = j
			return
		}
	}
}

func (m *Model) moveCursor(by int) {
	m.cursor = min(max(m.cursor+by, 0), max(len(m.visible())-1, 0))
}

func (m Model) openSelected() (tea.Model, tea.Cmd) {
	vis := m.visible()
	if m.cursor >= len(vis) {
		return m, nil
	}
	i := vis[m.cursor]
	m.chats[i].Unread = 0
	chat := m.chats[i]
	if m.searching {
		m.endSearch()
	}
	m.open = &chat
	m.messages = nil
	m.replyTo = nil
	m.detach()
	m.noOlder, m.loadingOlder = false, false
	m.render()
	m.setFocus(focusInput)
	return m, m.loadHistory(chat)
}

func (m Model) handleInputKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	if k.Paste {
		// An image on the clipboard pastes as nothing, so an empty paste
		// is the cue to look for one.
		if strings.TrimSpace(string(k.Runes)) == "" {
			return m, readClipboard()
		}
		if a := imageFile(string(k.Runes)); a != nil {
			m.attach(a)
			return m, nil
		}
	}

	switch k.String() {
	case "ctrl+v":
		return m, readClipboard()
	case "esc":
		switch {
		case m.attached != nil:
			m.detach()
		case m.replyTo != nil:
			m.replyTo = nil
		default:
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
		if m.open == nil || (text == "" && m.attached == nil) {
			return m, nil
		}
		if strings.HasPrefix(text, "/") && !strings.HasPrefix(text, "//") {
			return m.runCommand(text)
		}
		// "//" sends text that starts with a slash, such as a bot command.
		if strings.HasPrefix(text, "//") {
			text = text[1:]
		}
		replyTo := 0
		if m.replyTo != nil {
			replyTo = m.replyTo.ID
		}
		m.input.Reset()
		m.replyTo = nil
		if a := m.attached; a != nil {
			m.attached = nil
			m.status = "Uploading " + telegram.ShortName(a.name) + "..."
			return m, m.sendImage(*m.open, *a, text, replyTo)
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

func (m *Model) handleEvent(ev telegram.Event) tea.Cmd {
	switch ev := ev.(type) {
	case telegram.NewMessage:
		// A message ends whatever its sender was doing.
		delete(m.typing[ev.ChatKey], ev.FromKey)
		return m.receive(ev.Message)

	case telegram.EditedMessage:
		if m.open == nil || m.open.Key != ev.ChatKey {
			return nil
		}
		for i := range m.messages {
			if m.messages[i].ID == ev.ID {
				m.messages[i] = ev.Message
				m.render()
				break
			}
		}

	case telegram.DeletedMessages:
		if m.open == nil {
			return nil
		}
		// Without a chat key the IDs belong to private chats and basic
		// groups, which never share IDs with channels.
		if ev.ChatKey != m.open.Key && (ev.ChatKey != "" || strings.HasPrefix(m.open.Key, "ch")) {
			return nil
		}
		gone := make(map[int]bool, len(ev.IDs))
		for _, id := range ev.IDs {
			gone[id] = true
		}
		kept := make([]telegram.Message, 0, len(m.messages))
		for _, msg := range m.messages {
			if !gone[msg.ID] {
				kept = append(kept, msg)
			}
		}
		if len(kept) == len(m.messages) {
			return nil
		}
		m.messages = kept
		if m.replyTo != nil && gone[m.replyTo.ID] {
			m.replyTo = nil
		}
		if m.focus == focusPick {
			if len(m.messages) == 0 {
				m.setFocus(focusInput)
			}
			m.pick = min(m.pick, max(len(m.messages)-1, 0))
		}
		m.render()

	case telegram.Typing:
		if ev.Stopped {
			delete(m.typing[ev.ChatKey], ev.UserKey)
			return nil
		}
		if m.typing[ev.ChatKey] == nil {
			m.typing[ev.ChatKey] = map[string]typer{}
		}
		m.typing[ev.ChatKey][ev.UserKey] = typer{who: ev.Who, action: ev.Action, until: time.Now().Add(typingFor)}
		return m.tickTyping()
	}
	return nil
}

// typer is someone doing something in a chat.
type typer struct {
	who, action string
	until       time.Time
}

// typingFor is how long a typing report holds. Clients repeat it every few
// seconds while typing goes on; Telegram's own apps give up after six.
const typingFor = 6 * time.Second

// tickTyping schedules the next expiry check while anyone is typing.
func (m *Model) tickTyping() tea.Cmd {
	if m.ticking || len(m.typing) == 0 {
		return nil
	}
	m.ticking = true
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return typingTick{} })
}

func (m *Model) pruneTyping(now time.Time) {
	for chat, users := range m.typing {
		for user, t := range users {
			if now.After(t.until) {
				delete(users, user)
			}
		}
		if len(users) == 0 {
			delete(m.typing, chat)
		}
	}
}

// typingLine says who is doing what in a chat: "Anna is typing",
// "Anna and Ben are typing", "3 people are typing".
func (m Model) typingLine(chatKey string) string {
	users := m.typing[chatKey]
	switch len(users) {
	case 0:
		return ""
	case 1:
		for _, t := range users {
			return t.who + " is " + t.action
		}
	case 2:
		var names []string
		for _, t := range users {
			names = append(names, t.who)
		}
		sort.Strings(names)
		return names[0] + " and " + names[1] + " are typing"
	}
	return fmt.Sprintf("%d people are typing", len(users))
}

// receive appends a message to the open chat, or bumps the unread counter of
// the chat it belongs to and moves that chat to the top.
func (m *Model) receive(msg telegram.Message) tea.Cmd {
	var cmds []tea.Cmd
	if m.open != nil && m.open.Key == msg.ChatKey && !hasMessage(m.messages, msg.ID) {
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
		selected, ok := m.selectedKey()
		copy(m.chats[1:i+1], m.chats[:i])
		m.chats[0] = c
		if ok {
			m.selectKey(selected)
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
	if m.help {
		m.view.SetContent(helpText())
		return
	}
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
		body := msg.Text
		if msg.Out {
			body = mine.Render(body)
		}
		if msg.Edited {
			body += dim.Render(" (edited)")
		}
		block.WriteString(wrap.Render(head + ": " + body))

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

	hint := dim.Render(truncate("/help or Ctrl+H: commands and keys", m.view.Width))
	switch {
	case m.help:
		hint = dim.Render("Any key closes the help")
	case m.focus == focusPick:
		hint = dim.Render("Up/Down: choose  Enter: reply  Esc: cancel")
	case m.attached != nil:
		label := "Image " + telegram.ShortName(m.attached.name)
		if m.replyTo != nil {
			label += " in reply to " + senderName(*m.replyTo)
		}
		hint = truncate(label, m.view.Width-37) + dim.Render(" (type a caption, Esc removes image)")
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
		if t := m.typingLine(m.open.Key); t != "" {
			title += "  (" + t + "...)"
		}
	}
	status := m.status
	if m.loadingOlder {
		status = "Loading older messages..."
	}
	body := lipgloss.JoinHorizontal(lipgloss.Top, list, chat)
	return body + "\n" + dim.Render(truncate(" "+title+"  |  "+status, m.width))
}

func (m Model) renderList(height int) string {
	var rows []string
	if m.searching {
		m.search.Width = listWidth - 2
		rows = append(rows, m.search.View())
		height--
	}
	vis := m.visible()
	if len(vis) == 0 {
		empty := "(no chats)"
		if m.searching {
			empty = "(no match)"
		}
		return strings.Join(append(rows, dim.Render(empty)), "\n")
	}
	// Keep the cursor visible by scrolling the window of rows.
	start := 0
	if m.cursor >= height {
		start = m.cursor - height + 1
	}
	for j := start; j < len(vis) && j < start+height; j++ {
		c := m.chats[vis[j]]
		marker := "  "
		if j == m.cursor {
			marker = "> "
		}
		unread := ""
		if c.Unread > 0 {
			unread = fmt.Sprintf(" (%d)", c.Unread)
		}
		row := truncate(marker+c.Title, listWidth-len(unread)) + unread
		if j == m.cursor {
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

// attach stages an image for the next message, replacing any staged one.
func (m *Model) attach(a *attachment) {
	m.detach()
	m.attached = a
	m.status = "Attached " + telegram.ShortName(a.name) + ". Enter sends it."
}

func (m *Model) detach() {
	if m.attached != nil {
		discard(m.attached)
		m.attached = nil
	}
}

// discard removes the file behind a clipboard image; files the user named
// are theirs and stay.
func discard(a *attachment) {
	if a.temp {
		os.Remove(a.path)
	}
}

func (m Model) sendImage(chat telegram.Chat, a attachment, caption string, replyTo int) tea.Cmd {
	return func() tea.Msg {
		defer discard(&a)
		if err := m.client.SendImage(m.ctx, chat, a.path, caption, replyTo); err != nil {
			return errMsg{err}
		}
		return sentMsg{chat}
	}
}

func (m Model) waitForUpdate() tea.Cmd {
	return func() tea.Msg {
		select {
		case ev := <-m.client.Events():
			return eventMsg{ev}
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

// mergeLatest puts a freshly loaded latest page after the loaded messages
// older than it, so reloading after a send keeps scrollback and shows each
// message once.
func mergeLatest(loaded, latest []telegram.Message) []telegram.Message {
	if len(latest) == 0 {
		return loaded
	}
	first := latest[0].ID
	merged := make([]telegram.Message, 0, len(loaded)+len(latest))
	for _, msg := range loaded {
		if msg.ID < first {
			merged = append(merged, msg)
		}
	}
	return append(merged, latest...)
}

func hasMessage(msgs []telegram.Message, id int) bool {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].ID == id {
			return true
		}
	}
	return false
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
