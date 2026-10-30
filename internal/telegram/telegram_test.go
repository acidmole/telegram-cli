package telegram

import (
	"testing"
	"time"
)

func TestShortName(t *testing.T) {
	for name, want := range map[string]string{
		"cat.png":                 "cat.png",
		"exactly14c.jpg":          "exactly14c.jpg",
		"holiday-beach-2026.jpg":  "holiday-b~.jpg",
		"scan.thisisaverylongext": "scan.thisisav~",
	} {
		got := ShortName(name)
		if got != want {
			t.Errorf("ShortName(%q) = %q, want %q", name, got, want)
		}
		if n := len([]rune(got)); n > 14 {
			t.Errorf("ShortName(%q) is %d characters", name, n)
		}
	}
}

func TestMediaKind(t *testing.T) {
	for path, want := range map[string]string{
		"cat.png":       "image",
		"/tmp/IMG.JPEG": "image",
		"clip.mp4":      "video",
		"screen.MOV":    "video",
		"notes.txt":     "",
		"noext":         "",
	} {
		if got := MediaKind(path); got != want {
			t.Errorf("MediaKind(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestParseProbe(t *testing.T) {
	w, h, d, ok := parseProbe("width=1920\nheight=1080\nduration=12.500000\n")
	if !ok || w != 1920 || h != 1080 || d != 12500*time.Millisecond {
		t.Errorf("parseProbe = %d, %d, %v, %v", w, h, d, ok)
	}
	if _, _, _, ok := parseProbe("width=1920\nheight=1080\nduration=N/A\n"); ok {
		t.Error("parseProbe accepted a video without a duration")
	}
}
