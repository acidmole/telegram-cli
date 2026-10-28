# telegram-cli

A Telegram client for the terminal, in plain ASCII. It logs in as your own
account (not a bot), built on [gotd/td](https://github.com/gotd/td) and
[Bubble Tea](https://github.com/charmbracelet/bubbletea).

```
+------------------------------+------------------------------------------+
|> Climbing club (2)           |Oct 28 14:02 Anna: who's in for Saturday? |
|  Saved Messages              |Oct 28 14:05 me: count me in              |
|  Ben                         || re Anna: who's in for Saturday?         |
|                              |Oct 28 14:06 Ben: me too [img: wall~.jpg] |
+------------------------------+------------------------------------------+
                               |/help or Ctrl+H: commands and keys        |
                               |> _                                       |
                               +------------------------------------------+
 Climbing club  (Anna is typing...)  |  2 chats. Ctrl+H: help
```

## Features

- Chat list with unread counts and search (`/`)
- Live messages, edits, deletions and typing indicators
- Replies (`Ctrl+R`), with the quoted message shown above the answer
- Images: `/img <path> [caption]`, or paste one with `Ctrl+V`
- Media shown as its file name, at most 14 characters: `[img: holiday-b~.jpg]`
- Chats marked read on all your devices when you open them
- Desktop notifications for unmuted chats (`-no-notify` turns them off)

## Setup

1. Get an `api_id` and `api_hash` at <https://my.telegram.org> under
   *API development tools*.
2. Install with Go 1.27 or later:

   ```sh
   go install github.com/acidmole/telegram-cli@latest
   ```

3. Run it:

   ```sh
   export TG_API_ID=123456
   export TG_API_HASH=0123456789abcdef0123456789abcdef
   telegram-cli
   ```

The first run asks for your phone number, the login code and your two-step
verification password, if you have one. The session is then kept in
`$XDG_DATA_HOME/telegram-cli/session.json` (by default
`~/.local/share/telegram-cli/`), readable only by you. It grants full access
to your account: keep it private, and delete it to log out.

Pasting images needs `wl-paste` (Wayland) and notifications need
`notify-send`.

## Keys

Press `Ctrl+H` or type `/help` in the app for the full list.

| Where     | Key                | Does                                     |
|-----------|--------------------|------------------------------------------|
| Chat list | `Up`/`Down`, `j`/`k` | move                                   |
|           | `Enter`            | open the chat                            |
|           | `/`                | search chats by name                     |
| Chat      | `Enter`            | send                                     |
|           | `Ctrl+R`           | reply to a message                       |
|           | `Ctrl+V`           | paste text or an image                   |
|           | `PgUp`/`PgDn`      | scroll; `PgUp` at the top loads older    |
|           | `Esc`              | drop image or reply, back to the list    |
| Anywhere  | `Tab`              | switch pane                              |
|           | `Ctrl+C`           | quit                                     |

In the input line a message starting with `/` is a command. Send `//text`
for a message that really starts with a slash.

## Development

```sh
mise install        # Go version from mise.toml
go test ./...
go build -o telegram-cli .
```
