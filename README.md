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
- Sender names in a stable color each; your own messages in a muted one
- Desktop notifications for unmuted chats

## Requirements

- Linux or another Unix-like system with a terminal that does 256 colors
- Go 1.27 or later, to build it
- A Telegram account, and an API ID for it (see below)
- Optional: `wl-paste` (from `wl-clipboard`) to paste images on Wayland,
  `notify-send` (from `libnotify`) for desktop notifications

## Installation

### With `go install`

```sh
go install github.com/acidmole/telegram-cli@latest
```

The binary lands in `$(go env GOPATH)/bin`, usually `~/go/bin`. Make sure
that directory is on your `PATH`.

### From source

```sh
git clone https://github.com/acidmole/telegram-cli.git
cd telegram-cli
go build -o telegram-cli .
./telegram-cli
```

If you use [mise](https://mise.jdx.dev), `mise install` in the checkout
fetches the Go version pinned in `mise.toml`.

## Telegram API credentials

Telegram requires every client to identify itself with an API ID and hash.
They belong to you, not to this project, so each user registers their own.

1. Open <https://my.telegram.org> and log in with your phone number. The
   code arrives in your Telegram app, not by SMS.
2. Choose **API development tools**.
3. Fill in the form. Only these fields matter:
   - **App title**: anything, e.g. `telegram-cli`
   - **Short name**: letters and digits only, 5 to 32 characters
   - **Platform**: Desktop. The URL and description can stay empty.
4. Submit. The page now shows your **api_id** (a number) and **api_hash**
   (32 hex characters).

If the form only answers "ERROR", try again with your VPN or ad blocker
turned off, or with a different short name; it is a long-standing quirk of
that page.

**Keep the api_hash private.** Anyone with it can pose as your app. Do not
commit it, paste it into issues, or put it in shared shell history.

### Giving them to telegram-cli

telegram-cli reads them from two environment variables:

```sh
export TG_API_ID=123456
export TG_API_HASH=0123456789abcdef0123456789abcdef
telegram-cli
```

To avoid typing them every time, keep them in a file only you can read and
load it from your shell's startup file:

```sh
cat > ~/.config/telegram-cli.env <<'EOF'
export TG_API_ID=123456
export TG_API_HASH=0123456789abcdef0123456789abcdef
EOF
chmod 600 ~/.config/telegram-cli.env
echo '[ -f ~/.config/telegram-cli.env ] && . ~/.config/telegram-cli.env' >> ~/.bashrc
```

### Logging in

The first run asks, in the terminal, for:

1. your phone number, in international format (`+358...`)
2. the login code Telegram sends to your other devices
3. your two-step verification password, if you have set one (not echoed)

The session is then saved in `$XDG_DATA_HOME/telegram-cli/session.json`,
by default `~/.local/share/telegram-cli/session.json`, readable only by
you. Later runs start straight into your chats.

The session file grants full access to your account, like a logged-in
phone. Keep it private. Delete it to log out on this machine, or end the
session from another device under *Settings > Devices*.

### Telegram's terms

Third-party clients are allowed, under the
[Telegram API Terms of Service](https://core.telegram.org/api/terms).
In short: use your own API ID, do not spam or automate bulk actions, and
respect flood limits. New API IDs on new accounts are sometimes flagged by
Telegram's anti-spam systems; if you are worried, try it on a spare account
first.

## Usage

```sh
telegram-cli              # normal run
telegram-cli -no-notify   # without desktop notifications
```

Press `Ctrl+H` or type `/help` in the app for the full list of keys and
commands.

| Where     | Key                  | Does                                  |
|-----------|----------------------|---------------------------------------|
| Chat list | `Up`/`Down`, `j`/`k` | move                                  |
|           | `Enter`              | open the chat                         |
|           | `/`                  | search chats by name, `Esc` clears    |
|           | `q`                  | quit                                  |
| Chat      | `Enter`              | send                                  |
|           | `Ctrl+R`             | reply to a message                    |
|           | `Ctrl+V`             | paste text or an image                |
|           | `PgUp`/`PgDn`        | scroll; `PgUp` at the top loads older |
|           | `Esc`                | drop image or reply, back to the list |
| Anywhere  | `Tab`                | switch pane                           |
|           | `Ctrl+H`             | help                                  |
|           | `Ctrl+C`             | quit                                  |

### Commands

In the input line, a message that starts with `/` is a command:

| Command                  | Does                            |
|--------------------------|---------------------------------|
| `/img <path> [caption]`  | send a jpg, png or webp image   |
| `/help`                  | show keys and commands          |
| `//text`                 | send text that starts with `/`  |

An unknown command is never sent; it stays in the input so you can fix it.

## Troubleshooting

- **"set TG_API_ID and TG_API_HASH"**: the variables are missing from the
  environment the program runs in. Check with `echo $TG_API_ID`.
- **The login code never arrives**: it goes to your Telegram app on
  another device, as a message from "Telegram", not by SMS.
- **`FLOOD_WAIT`** errors: Telegram is rate-limiting the account. Wait the
  number of seconds it names before trying again.
- **Ctrl+V pastes nothing**: install `wl-clipboard`. Only png, jpeg and
  webp images can be sent.
- **Backspace opens the help**: your terminal sends `^H` for Backspace,
  which is also `Ctrl+H`. Set it to send `^?` (DEL), or use `/help`.

## Development

```sh
go test ./...
go vet ./...
go build -o telegram-cli .
```

`internal/telegram` wraps gotd/td (chats, history, sending, updates);
`internal/ui` is the Bubble Tea interface; `main.go` handles configuration
and login.
