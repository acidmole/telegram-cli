package ui

import (
	"bytes"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// attachment is an image waiting to be sent with the next message.
type attachment struct {
	path string
	name string
	// temp marks a file written from the clipboard, removed once used.
	temp bool
}

// clipboardMsg reports what a paste turned out to hold: an image to attach,
// or text for the input line.
type clipboardMsg struct {
	image *attachment
	text  string
}

// clipboardImageTypes maps the image formats Telegram takes as photos to
// the extension of the file they are saved in.
var clipboardImageTypes = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/webp": ".webp",
}

// readClipboard asks wl-paste for an image on the Wayland clipboard, and
// falls back to its text when there is none.
func readClipboard() tea.Cmd {
	return func() tea.Msg {
		out, err := exec.Command("wl-paste", "--list-types").Output()
		if err != nil {
			return errMsg{fmt.Errorf("reading the clipboard needs wl-paste: %w", err)}
		}
		types := strings.Fields(string(out))
		for _, t := range types {
			ext, ok := clipboardImageTypes[t]
			if !ok {
				continue
			}
			data, err := exec.Command("wl-paste", "--no-newline", "--type", t).Output()
			if err != nil {
				return errMsg{err}
			}
			f, err := os.CreateTemp("", "telegram-cli-*"+ext)
			if err != nil {
				return errMsg{err}
			}
			_, err = f.Write(data)
			if cerr := f.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				os.Remove(f.Name())
				return errMsg{err}
			}
			return clipboardMsg{image: &attachment{path: f.Name(), name: "clipboard" + ext, temp: true}}
		}
		for _, t := range types {
			if strings.HasPrefix(t, "image/") {
				return errMsg{fmt.Errorf("clipboard image is %s; only png, jpeg and webp can be sent", t)}
			}
		}

		text, err := exec.Command("wl-paste", "--no-newline").Output()
		if err != nil {
			return errMsg{err}
		}
		if a := imageFile(string(text)); a != nil {
			return clipboardMsg{image: a}
		}
		return clipboardMsg{text: string(bytes.TrimRight(text, "\n"))}
	}
}

// imageFile recognizes pasted text that names a single image file, as a
// file manager's copy does ("file:///home/me/cat.png" or a plain path).
func imageFile(text string) *attachment {
	text = strings.TrimSpace(text)
	if text == "" || strings.ContainsAny(text, "\n\r") {
		return nil
	}
	path := text
	if u, err := url.Parse(text); err == nil && u.Scheme == "file" {
		path = u.Path
	}
	path = expandHome(path)
	if imageType(path) == "" {
		return nil
	}
	if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
		return nil
	}
	return &attachment{path: path, name: filepath.Base(path)}
}

func imageType(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	}
	return ""
}
