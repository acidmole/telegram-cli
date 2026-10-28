// Package telegram wraps gotd/td with the few operations the TUI needs:
// listing chats, reading history, sending text and streaming new messages.
package telegram

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/telegram/message/styling"
	"github.com/gotd/td/telegram/updates"
	updhook "github.com/gotd/td/telegram/updates/hook"
	"github.com/gotd/td/telegram/uploader"
	"github.com/gotd/td/tg"
)

// Chat is one entry of the chat list.
type Chat struct {
	Key    string
	Title  string
	Unread int
	Muted  bool
	Peer   tg.InputPeerClass
}

// Message is a text message as the TUI shows it.
type Message struct {
	ID      int
	ChatKey string
	FromKey string
	From    string
	Text    string
	Date    time.Time
	Out     bool
	// ReplyTo is the ID of the message this one answers, or 0.
	ReplyTo int
}

// Client is a logged-in Telegram session.
type Client struct {
	api     *tg.Client
	sender  *message.Sender
	gaps    *updates.Manager
	updates chan Message

	mu    sync.Mutex
	names map[string]string
}

// NewUpdateHandler returns the gap-aware update handler to pass to
// telegram.Options.UpdateHandler, and the Client that receives from it once
// Attach is called.
func NewUpdateHandler() (telegram.UpdateHandler, *Client) {
	c := &Client{
		updates: make(chan Message, 64),
		names:   map[string]string{},
	}
	d := tg.NewUpdateDispatcher()
	d.OnNewMessage(func(ctx context.Context, e tg.Entities, u *tg.UpdateNewMessage) error {
		c.handleNew(e, u.Message)
		return nil
	})
	d.OnNewChannelMessage(func(ctx context.Context, e tg.Entities, u *tg.UpdateNewChannelMessage) error {
		c.handleNew(e, u.Message)
		return nil
	})
	c.gaps = updates.New(updates.Config{Handler: d})
	return c.gaps, c
}

// Middleware feeds the updates that method results carry, such as the one
// for a message just sent, to the update tracker. Without it the tracker
// sees a gap after every send, fetches the difference and delivers our own
// message once more.
func (c *Client) Middleware() telegram.Middleware {
	return updhook.UpdateHook(c.gaps.Handle)
}

// Attach binds the client to an authorized connection and starts receiving
// updates. It blocks until ctx is done, so run it in its own goroutine.
func (c *Client) Attach(ctx context.Context, tc *telegram.Client, selfID int64) error {
	c.api = tc.API()
	c.sender = message.NewSender(c.api)
	return c.gaps.Run(ctx, c.api, selfID, updates.AuthOptions{})
}

// Updates delivers incoming messages from every chat.
func (c *Client) Updates() <-chan Message { return c.updates }

// Chats returns the most recent chats, newest first.
func (c *Client) Chats(ctx context.Context) ([]Chat, error) {
	res, err := c.api.MessagesGetDialogs(ctx, &tg.MessagesGetDialogsRequest{
		OffsetPeer: &tg.InputPeerEmpty{},
		Limit:      100,
	})
	if err != nil {
		return nil, err
	}
	dialogs, ok := res.AsModified()
	if !ok {
		return nil, fmt.Errorf("unexpected dialogs response %T", res)
	}
	peers := c.remember(dialogs.GetUsers(), dialogs.GetChats())

	chats := make([]Chat, 0, len(dialogs.GetDialogs()))
	for _, dc := range dialogs.GetDialogs() {
		d, ok := dc.(*tg.Dialog)
		if !ok {
			continue
		}
		key := peerKey(d.Peer)
		p, ok := peers[key]
		if !ok {
			continue
		}
		muteUntil, _ := d.NotifySettings.GetMuteUntil()
		chats = append(chats, Chat{
			Key:    key,
			Title:  c.name(key),
			Unread: d.UnreadCount,
			Muted:  int64(muteUntil) > time.Now().Unix(),
			Peer:   p,
		})
	}
	return chats, nil
}

// HistoryPage is how many messages History returns at most.
const HistoryPage = 50

// History returns up to HistoryPage messages of a chat, oldest first. With
// beforeID 0 they are the latest ones, otherwise the ones older than beforeID.
func (c *Client) History(ctx context.Context, chat Chat, beforeID int) ([]Message, error) {
	res, err := c.api.MessagesGetHistory(ctx, &tg.MessagesGetHistoryRequest{
		Peer:     chat.Peer,
		OffsetID: beforeID,
		Limit:    HistoryPage,
	})
	if err != nil {
		return nil, err
	}
	history, ok := res.AsModified()
	if !ok {
		return nil, fmt.Errorf("unexpected history response %T", res)
	}
	c.remember(history.GetUsers(), history.GetChats())

	raw := history.GetMessages()
	msgs := make([]Message, 0, len(raw))
	for i := len(raw) - 1; i >= 0; i-- {
		if m, ok := c.convert(raw[i]); ok {
			msgs = append(msgs, m)
		}
	}
	return msgs, nil
}

// Send posts a text message to a chat, as a reply when replyTo is not 0.
func (c *Client) Send(ctx context.Context, chat Chat, text string, replyTo int) error {
	b := &c.sender.To(chat.Peer).Builder
	if replyTo != 0 {
		b = b.Reply(replyTo)
	}
	_, err := b.Text(ctx, text)
	return err
}

// imageExts are the formats Telegram accepts as photos.
var imageExts = map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".webp": true}

// SendImage uploads the image at path and posts it as a photo with an
// optional caption, as a reply when replyTo is not 0.
func (c *Client) SendImage(ctx context.Context, chat Chat, path, caption string, replyTo int) error {
	if !imageExts[strings.ToLower(filepath.Ext(path))] {
		return fmt.Errorf("%s: not a jpg, png or webp image", filepath.Base(path))
	}
	file, err := uploader.NewUploader(c.api).FromPath(ctx, path)
	if err != nil {
		return err
	}
	b := &c.sender.To(chat.Peer).Builder
	if replyTo != 0 {
		b = b.Reply(replyTo)
	}
	_, err = b.Media(ctx, message.UploadedPhoto(file, styling.Plain(caption)))
	return err
}

// MarkRead marks the chat read up to and including message maxID, on every
// device of the account.
func (c *Client) MarkRead(ctx context.Context, chat Chat, maxID int) error {
	if ch, ok := chat.Peer.(*tg.InputPeerChannel); ok {
		_, err := c.api.ChannelsReadHistory(ctx, &tg.ChannelsReadHistoryRequest{
			Channel: &tg.InputChannel{ChannelID: ch.ChannelID, AccessHash: ch.AccessHash},
			MaxID:   maxID,
		})
		return err
	}
	_, err := c.api.MessagesReadHistory(ctx, &tg.MessagesReadHistoryRequest{Peer: chat.Peer, MaxID: maxID})
	return err
}

func (c *Client) handleNew(e tg.Entities, mc tg.MessageClass) {
	users := make([]tg.UserClass, 0, len(e.Users))
	for _, u := range e.Users {
		users = append(users, u)
	}
	chats := make([]tg.ChatClass, 0, len(e.Chats)+len(e.Channels))
	for _, ch := range e.Chats {
		chats = append(chats, ch)
	}
	for _, ch := range e.Channels {
		chats = append(chats, ch)
	}
	c.remember(users, chats)

	if m, ok := c.convert(mc); ok {
		// Never block the update loop on a slow UI; drop instead.
		select {
		case c.updates <- m:
		default:
		}
	}
}

// remember caches display names and returns the input peers it could build,
// keyed like peerKey.
func (c *Client) remember(users []tg.UserClass, chats []tg.ChatClass) map[string]tg.InputPeerClass {
	c.mu.Lock()
	defer c.mu.Unlock()

	peers := map[string]tg.InputPeerClass{}
	for _, uc := range users {
		u, ok := uc.(*tg.User)
		if !ok {
			continue
		}
		key := fmt.Sprintf("u%d", u.ID)
		name := strings.TrimSpace(u.FirstName + " " + u.LastName)
		if name == "" {
			name = u.Username
		}
		if u.Self {
			name = "Saved Messages"
		}
		c.names[key] = name
		peers[key] = &tg.InputPeerUser{UserID: u.ID, AccessHash: u.AccessHash}
	}
	for _, cc := range chats {
		switch ch := cc.(type) {
		case *tg.Chat:
			key := fmt.Sprintf("c%d", ch.ID)
			c.names[key] = ch.Title
			peers[key] = &tg.InputPeerChat{ChatID: ch.ID}
		case *tg.Channel:
			key := fmt.Sprintf("ch%d", ch.ID)
			c.names[key] = ch.Title
			peers[key] = &tg.InputPeerChannel{ChannelID: ch.ID, AccessHash: ch.AccessHash}
		}
	}
	return peers
}

func (c *Client) name(key string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if n, ok := c.names[key]; ok && n != "" {
		return n
	}
	return key
}

func (c *Client) convert(mc tg.MessageClass) (Message, bool) {
	m, ok := mc.(*tg.Message)
	if !ok {
		return Message{}, false
	}
	chatKey := peerKey(m.PeerID)
	from := chatKey
	if f, ok := m.GetFromID(); ok {
		from = peerKey(f)
	}
	replyTo := 0
	if h, ok := m.ReplyTo.(*tg.MessageReplyHeader); ok {
		replyTo, _ = h.GetReplyToMsgID()
	}
	text := m.Message
	if m.Media != nil {
		text = strings.TrimSpace(mediaLabel(m.Media) + " " + text)
	}
	return Message{
		ID:      m.ID,
		ChatKey: chatKey,
		FromKey: from,
		From:    c.name(from),
		Text:    text,
		Date:    time.Unix(int64(m.Date), 0),
		Out:     m.Out,
		ReplyTo: replyTo,
	}, true
}

func mediaLabel(media tg.MessageMediaClass) string {
	switch media := media.(type) {
	case *tg.MessageMediaPhoto:
		// Telegram keeps no file name for photos.
		return "[img: photo.jpg]"
	case *tg.MessageMediaDocument:
		doc, ok := media.Document.(*tg.Document)
		if !ok {
			return "[file]"
		}
		name := "file"
		for _, a := range doc.Attributes {
			if f, ok := a.(*tg.DocumentAttributeFilename); ok {
				name = f.FileName
			}
		}
		if strings.HasPrefix(doc.MimeType, "image/") {
			return "[img: " + ShortName(name) + "]"
		}
		return "[file: " + ShortName(name) + "]"
	case *tg.MessageMediaGeo, *tg.MessageMediaGeoLive, *tg.MessageMediaVenue:
		return "[location]"
	case *tg.MessageMediaContact:
		return "[contact]"
	case *tg.MessageMediaPoll:
		return "[poll]"
	case *tg.MessageMediaWebPage:
		return ""
	default:
		return "[media]"
	}
}

// ShortName fits a file name in 14 characters, keeping its extension:
// "holiday-beach-2026.jpg" becomes "holiday-b~.jpg".
func ShortName(name string) string {
	const limit = 14
	r := []rune(name)
	if len(r) <= limit {
		return name
	}
	ext := []rune(filepath.Ext(name))
	if len(ext) >= limit-1 {
		return string(r[:limit-1]) + "~"
	}
	keep := limit - len(ext) - 1
	return string(r[:keep]) + "~" + string(ext)
}

func peerKey(p tg.PeerClass) string {
	switch p := p.(type) {
	case *tg.PeerUser:
		return fmt.Sprintf("u%d", p.UserID)
	case *tg.PeerChat:
		return fmt.Sprintf("c%d", p.ChatID)
	case *tg.PeerChannel:
		return fmt.Sprintf("ch%d", p.ChannelID)
	default:
		return ""
	}
}
