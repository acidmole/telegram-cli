// Command telegram-cli is a terminal Telegram client.
//
// It reads TG_API_ID and TG_API_HASH (from https://my.telegram.org) from the
// environment and keeps the login session in
// $XDG_DATA_HOME/telegram-cli/session.json. With -no-notify it shows no
// desktop notifications.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gotd/td/session"
	gotd "github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/tg"
	"golang.org/x/term"

	"github.com/acidmole/telegram-cli/internal/telegram"
	"github.com/acidmole/telegram-cli/internal/ui"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "telegram-cli:", err)
		os.Exit(1)
	}
}

func run() error {
	noNotify := flag.Bool("no-notify", false, "do not show desktop notifications")
	flag.Parse()

	apiID, err := strconv.Atoi(os.Getenv("TG_API_ID"))
	if err != nil {
		return errors.New("set TG_API_ID and TG_API_HASH from https://my.telegram.org")
	}
	apiHash := os.Getenv("TG_API_HASH")
	if apiHash == "" {
		return errors.New("set TG_API_HASH from https://my.telegram.org")
	}

	sessionPath, err := sessionFile()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	handler, client := telegram.NewUpdateHandler()
	tc := gotd.NewClient(apiID, apiHash, gotd.Options{
		SessionStorage: &session.FileStorage{Path: sessionPath},
		UpdateHandler:  handler,
		Middlewares:    []gotd.Middleware{client.Middleware()},
	})

	return tc.Run(ctx, func(ctx context.Context) error {
		flow := auth.NewFlow(terminalAuth{in: bufio.NewReader(os.Stdin)}, auth.SendCodeOptions{})
		if err := tc.Auth().IfNecessary(ctx, flow); err != nil {
			return fmt.Errorf("login: %w", err)
		}
		self, err := tc.Self(ctx)
		if err != nil {
			return err
		}

		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		updatesDone := make(chan error, 1)
		go func() { updatesDone <- client.Attach(ctx, tc, self.ID) }()

		_, err = tea.NewProgram(ui.New(ctx, client, ui.Options{Notify: !*noNotify}), tea.WithAltScreen(), tea.WithContext(ctx)).Run()
		cancel()
		<-updatesDone
		if errors.Is(err, tea.ErrProgramKilled) || errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	})
}

// sessionFile returns the session path, creating its directory readable only
// by the user: the session grants full access to the account.
func sessionFile() (string, error) {
	dir := os.Getenv("XDG_DATA_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".local", "share")
	}
	dir = filepath.Join(dir, "telegram-cli")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return filepath.Join(dir, "session.json"), nil
}

// terminalAuth asks for login details on the terminal before the TUI starts.
type terminalAuth struct {
	in *bufio.Reader
}

func (a terminalAuth) Phone(_ context.Context) (string, error) {
	return a.prompt("Phone number (international format, e.g. +358...): ")
}

func (a terminalAuth) Code(_ context.Context, _ *tg.AuthSentCode) (string, error) {
	return a.prompt("Code sent to your Telegram app: ")
}

func (a terminalAuth) Password(_ context.Context) (string, error) {
	fmt.Print("Two-step verification password: ")
	pw, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	return string(pw), err
}

func (a terminalAuth) AcceptTermsOfService(_ context.Context, tos tg.HelpTermsOfService) error {
	fmt.Println(tos.Text)
	answer, err := a.prompt("Accept Telegram's terms of service? [y/N]: ")
	if err != nil {
		return err
	}
	if !strings.EqualFold(answer, "y") {
		return errors.New("terms of service not accepted")
	}
	return nil
}

func (a terminalAuth) SignUp(_ context.Context) (auth.UserInfo, error) {
	return auth.UserInfo{}, errors.New("this phone number has no Telegram account; sign up in an official app first")
}

func (a terminalAuth) prompt(label string) (string, error) {
	fmt.Print(label)
	line, err := a.in.ReadString('\n')
	return strings.TrimSpace(line), err
}
