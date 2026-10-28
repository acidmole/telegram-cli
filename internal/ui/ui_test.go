package ui

import (
	"reflect"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/acidmole/telegram-cli/internal/telegram"
)

func ids(msgs []telegram.Message) []int {
	out := []int{}
	for _, m := range msgs {
		out = append(out, m.ID)
	}
	return out
}

func page(ids ...int) []telegram.Message {
	msgs := make([]telegram.Message, len(ids))
	for i, id := range ids {
		msgs[i] = telegram.Message{ID: id}
	}
	return msgs
}

func TestMergeLatest(t *testing.T) {
	for _, tc := range []struct {
		name           string
		loaded, latest []telegram.Message
		want           []int
	}{
		{"first load", nil, page(5, 6), []int{5, 6}},
		{"own message already appended by an update", page(1, 2, 3, 4, 5, 6, 7), page(5, 6, 7), []int{1, 2, 3, 4, 5, 6, 7}},
		{"scrollback kept, new message added", page(1, 2, 5, 6), page(5, 6, 7), []int{1, 2, 5, 6, 7}},
		{"empty reload keeps what is shown", page(1, 2), nil, []int{1, 2}},
	} {
		if got := ids(mergeLatest(tc.loaded, tc.latest)); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestReceiveSkipsShownMessage(t *testing.T) {
	chat := telegram.Chat{Key: "u1"}
	m := New(t.Context(), nil, Options{})
	m.open = &chat
	m.chats = []telegram.Chat{chat}
	m.messages = page(1, 2)
	for i := range m.messages {
		m.messages[i].ChatKey = "u1"
	}

	m.receive(telegram.Message{ID: 2, ChatKey: "u1", Out: true})

	if got := ids(m.messages); !reflect.DeepEqual(got, []int{1, 2}) {
		t.Errorf("got %v, want [1 2]", got)
	}
}

func openModel(t *testing.T, key string, msgIDs ...int) Model {
	t.Helper()
	chat := telegram.Chat{Key: key}
	m := New(t.Context(), nil, Options{})
	m.open = &chat
	m.chats = []telegram.Chat{chat}
	m.messages = page(msgIDs...)
	for i := range m.messages {
		m.messages[i].ChatKey = key
	}
	return m
}

func TestSearchFiltersAndOpens(t *testing.T) {
	m := New(t.Context(), nil, Options{})
	m.chats = []telegram.Chat{{Key: "u1", Title: "Anna"}, {Key: "c2", Title: "Climbing club"}, {Key: "u3", Title: "Hanna"}}

	var model tea.Model = m
	for _, k := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune("/")},
		{Type: tea.KeyRunes, Runes: []rune("ANN")},
		{Type: tea.KeyDown},
	} {
		model, _ = model.Update(k)
	}
	m = model.(Model)
	if key, _ := m.selectedKey(); key != "u3" {
		t.Fatalf("selected %q after searching ANN and moving down, want u3", key)
	}
	if n := len(m.visible()); n != 2 {
		t.Fatalf("%d chats match ANN, want 2", n)
	}

	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = model.(Model)
	if m.open == nil || m.open.Key != "u3" {
		t.Fatalf("opened %v, want u3", m.open)
	}
	if m.searching || len(m.visible()) != 3 {
		t.Errorf("search still active after opening a chat")
	}
	if key, _ := m.selectedKey(); key != "u3" {
		t.Errorf("selection moved to %q after the search ended, want u3", key)
	}
}

func TestEditReplacesMessage(t *testing.T) {
	m := openModel(t, "u1", 1, 2)
	m.handleEvent(telegram.EditedMessage{Message: telegram.Message{ID: 2, ChatKey: "u1", Text: "fixed", Edited: true}})
	if got := m.messages[1]; got.Text != "fixed" || !got.Edited || len(m.messages) != 2 {
		t.Errorf("got %+v, want message 2 replaced by its edit", m.messages)
	}
}

func TestDeleteRemovesMessages(t *testing.T) {
	for _, tc := range []struct {
		name, open, from string
		want             []int
	}{
		{"private chat, no chat key", "u1", "", []int{1, 3}},
		{"same channel", "ch5", "ch5", []int{1, 3}},
		{"other channel", "ch5", "ch6", []int{1, 2, 3}},
		{"channel open, IDs from a private chat", "ch5", "", []int{1, 2, 3}},
	} {
		m := openModel(t, tc.open, 1, 2, 3)
		m.handleEvent(telegram.DeletedMessages{ChatKey: tc.from, IDs: []int{2}})
		if got := ids(m.messages); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestTypingExpires(t *testing.T) {
	m := openModel(t, "c1")
	m.handleEvent(telegram.Typing{ChatKey: "c1", UserKey: "u2", Who: "Ben", Action: "typing"})
	m.handleEvent(telegram.Typing{ChatKey: "c1", UserKey: "u3", Who: "Anna", Action: "typing"})
	if got := m.typingLine("c1"); got != "Anna and Ben are typing" {
		t.Errorf("got %q", got)
	}

	m.handleEvent(telegram.NewMessage{Message: telegram.Message{ID: 9, ChatKey: "c1", FromKey: "u2"}})
	if got := m.typingLine("c1"); got != "Anna is typing" {
		t.Errorf("after Ben's message got %q, want Anna is typing", got)
	}

	m.pruneTyping(time.Now().Add(typingFor + time.Second))
	if got := m.typingLine("c1"); got != "" {
		t.Errorf("after expiry got %q, want nothing", got)
	}
}
