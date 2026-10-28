package ui

import (
	"reflect"
	"testing"

	"telegram-cli/internal/telegram"
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
